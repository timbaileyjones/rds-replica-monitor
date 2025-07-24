package main

import (
	"database/sql"
	"flag"
	"fmt"
	"log"
	"regexp"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

// Command line flags
var (
	host     string
	user     string
	password string
	port     int
)

// Track replication lag statistics
type ReplicationStats struct {
	lastSecondsBehind int
	lastCheckTime     time.Time
	ratePerSecond     float64 // short-term rate (last interval)
	estimatedTime     time.Time

	// Long-term tracking
	startSecondsBehind   int
	startTime            time.Time
	totalTimeElapsed     float64
	averageRatePerSecond float64 // long-term average rate
}

var replicationStats ReplicationStats

func main() {
	// Parse command line flags
	flag.StringVar(&host, "host", "", "MySQL host (required)")
	flag.StringVar(&user, "user", "", "MySQL username (required)")
	flag.StringVar(&password, "password", "", "MySQL password (required)")
	flag.IntVar(&port, "port", 3306, "MySQL port (default: 3306)")
	flag.Parse()

	// Validate required parameters
	if host == "" || user == "" || password == "" {
		fmt.Println("Usage: replica-monitor -host <hostname> -user <username> -password <password> [-port <port>]")
		fmt.Println("Example: replica-monitor -host mydb.example.com -user admin -password mypass")
		flag.PrintDefaults()
		return
	}

	// Create connection string
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/", user, password, host, port)

	// Connect to database
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Close()

	// Test the connection
	err = db.Ping()
	if err != nil {
		log.Fatalf("Failed to ping database: %v", err)
	}

	fmt.Printf("Successfully connected to MySQL database at %s:%d\n", host, port)
	fmt.Println("Starting replica status monitoring...")
	fmt.Println("Press Ctrl+C to stop")
	fmt.Println()

	// Main monitoring loop
	for {
		hasError := showReplicaStatus(db)
		if hasError {
			fmt.Println("⚠️  WARNING: SQL Error detected!")
			fmt.Println("🔄 Executing mysql.rds_skip_repl_error...")

			// Execute the skip error command
			_, err := db.Exec("CALL mysql.rds_skip_repl_error;")
			if err != nil {
				log.Printf("Error executing mysql.rds_skip_repl_error: %v", err)
			} else {
				fmt.Println("✅ Successfully executed mysql.rds_skip_repl_error")
			}

			// Skip rest of the loop for this iteration
			continue
		}
		time.Sleep(5 * time.Second) // Wait 5 seconds between checks
	}
}

func showReplicaStatus(db *sql.DB) bool {
	now := time.Now()
	rows, err := db.Query("SHOW REPLICA STATUS")
	if err != nil {
		log.Printf("Error executing SHOW REPLICA STATUS: %v", err)
		return false
	}
	defer rows.Close()

	// Get column names
	columns, err := rows.Columns()
	if err != nil {
		log.Printf("Error getting columns: %v", err)
		return false
	}

	// Create a slice to hold the values
	values := make([]interface{}, len(columns))
	valuePtrs := make([]interface{}, len(columns))
	for i := range values {
		valuePtrs[i] = &values[i]
	}

	// Define error patterns to check
	errorPatterns := []string{
		"Coordinator stopped",
	}

	// Read the data
	if rows.Next() {
		err := rows.Scan(valuePtrs...)
		if err != nil {
			log.Printf("Error scanning row: %v", err)
			return false
		}

		// Print timestamp
		fmt.Printf("\n[%s] Replica Status:\n", time.Now().Format("2006-01-02 15:04:05"))
		fmt.Println(strings.Repeat("=", 50))

		var lastSQLError string
		var hasError bool

		// Print key fields
		keyFields := []string{
			"Replica_IO_State",
			"Source_Host",
			"Source_Port",
			"Replica_IO_Running",
			"Replica_SQL_Running",
			"Replicate_Do_DB",
			"Replicate_Ignore_DB",
			"Last_IO_Error",
			"Last_SQL_Error",
			"Seconds_Behind_Source",
		}

		for _, field := range keyFields {
			for i, col := range columns {
				if col == field {
					val := values[i]
					if val != nil {
						// Convert to string properly
						var strVal string
						switch v := val.(type) {
						case []byte:
							strVal = string(v)
						case string:
							strVal = v
						default:
							strVal = fmt.Sprintf("%v", v)
						}

						// Store Last_SQL_Error for pattern checking
						if field == "Last_SQL_Error" {
							lastSQLError = strVal
						}

						// Format Seconds_Behind_Source specially
						if field == "Seconds_Behind_Source" {
							if strVal != "NULL" && strVal != "" {
								var seconds int
								if _, err := fmt.Sscanf(strVal, "%d", &seconds); err == nil {
									// Initialize start time and values on first run
									if replicationStats.startTime == (time.Time{}) {
										replicationStats.startSecondsBehind = seconds
										replicationStats.startTime = now
									}

									// Calculate short-term rate of change if we have previous data
									if replicationStats.lastCheckTime != (time.Time{}) {
										timeDiff := now.Sub(replicationStats.lastCheckTime).Seconds()
										if timeDiff > 0 {
											secondsDiff := seconds - replicationStats.lastSecondsBehind
											replicationStats.ratePerSecond = float64(secondsDiff) / timeDiff

											// Calculate short-term estimated time to catch up
											if replicationStats.ratePerSecond < 0 { // Negative means catching up
												secondsToCatchUp := float64(seconds) / -replicationStats.ratePerSecond
												replicationStats.estimatedTime = now.Add(time.Duration(secondsToCatchUp) * time.Second)
											}
										}
									}

									// Calculate long-term average rate
									totalTimeElapsed := now.Sub(replicationStats.startTime).Seconds()
									if totalTimeElapsed > 0 {
										totalSecondsDiff := seconds - replicationStats.startSecondsBehind
										replicationStats.averageRatePerSecond = float64(totalSecondsDiff) / totalTimeElapsed
									}

									// Update stats for next iteration
									replicationStats.lastSecondsBehind = seconds
									replicationStats.lastCheckTime = now

									if seconds > 0 {
										days := seconds / 86400
										hours := (seconds % 86400) / 3600
										minutes := (seconds % 3600) / 60
										secs := seconds % 60

										if days > 0 {
											fmt.Printf("%s: %dd %dh %dm %ds\n", field, days, hours, minutes, secs)
										} else if hours > 0 {
											fmt.Printf("%s: %dh %dm %ds\n", field, hours, minutes, secs)
										} else if minutes > 0 {
											fmt.Printf("%s: %dm %ds\n", field, minutes, secs)
										} else {
											fmt.Printf("%s: %ds\n", field, secs)
										}
									} else {
										fmt.Printf("%s: %ds (caught up!)\n", field, seconds)
									}

									// Display rates and estimates
									fmt.Println("📊 Replication Performance:")

									// Short-term rate (like instant MPG)
									if replicationStats.ratePerSecond != 0 {
										if replicationStats.ratePerSecond < 0 {
											fmt.Printf("  🚀 Instant: Catching up at %.2f seconds/second\n", -replicationStats.ratePerSecond)
											if !replicationStats.estimatedTime.IsZero() {
												eta := replicationStats.estimatedTime.Sub(now)
												etaDays := int(eta.Hours() / 24)
												etaHours := int(eta.Hours()) % 24
												etaMinutes := int(eta.Minutes()) % 60
												etaSeconds := int(eta.Seconds()) % 60

												if etaDays > 0 {
													fmt.Printf("  ⏰ Instant ETA: %dd %dh %dm %ds (%s)\n",
														etaDays, etaHours, etaMinutes, etaSeconds,
														replicationStats.estimatedTime.Format("2006-01-02 15:04:05"))
												} else if etaHours > 0 {
													fmt.Printf("  ⏰ Instant ETA: %dh %dm %ds (%s)\n",
														etaHours, etaMinutes, etaSeconds,
														replicationStats.estimatedTime.Format("2006-01-02 15:04:05"))
												} else if etaMinutes > 0 {
													fmt.Printf("  ⏰ Instant ETA: %dm %ds (%s)\n",
														etaMinutes, etaSeconds,
														replicationStats.estimatedTime.Format("2006-01-02 15:04:05"))
												} else {
													fmt.Printf("  ⏰ Instant ETA: %ds (%s)\n",
														etaSeconds,
														replicationStats.estimatedTime.Format("2006-01-02 15:04:05"))
												}
											}
										} else {
											fmt.Printf("  ⚠️  Instant: Falling behind at %.2f seconds/second\n", replicationStats.ratePerSecond)
										}
									}

									// Long-term average rate (like average MPG)
									if replicationStats.averageRatePerSecond != 0 {
										if replicationStats.averageRatePerSecond < 0 {
											fmt.Printf("  📈 Average: Catching up at %.2f seconds/second\n", -replicationStats.averageRatePerSecond)

											// Calculate long-term estimate
											if seconds > 0 {
												secondsToCatchUp := float64(seconds) / -replicationStats.averageRatePerSecond
												averageETA := now.Add(time.Duration(secondsToCatchUp) * time.Second)
												eta := averageETA.Sub(now)
												etaDays := int(eta.Hours() / 24)
												etaHours := int(eta.Hours()) % 24
												etaMinutes := int(eta.Minutes()) % 60
												etaSeconds := int(eta.Seconds()) % 60

												if etaDays > 0 {
													fmt.Printf("  ⏰ Average ETA: %dd %dh %dm %ds (%s)\n",
														etaDays, etaHours, etaMinutes, etaSeconds,
														averageETA.Format("2006-01-02 15:04:05"))
												} else if etaHours > 0 {
													fmt.Printf("  ⏰ Average ETA: %dh %dm %ds (%s)\n",
														etaHours, etaMinutes, etaSeconds,
														averageETA.Format("2006-01-02 15:04:05"))
												} else if etaMinutes > 0 {
													fmt.Printf("  ⏰ Average ETA: %dm %ds (%s)\n",
														etaMinutes, etaSeconds,
														averageETA.Format("2006-01-02 15:04:05"))
												} else {
													fmt.Printf("  ⏰ Average ETA: %ds (%s)\n",
														etaSeconds,
														averageETA.Format("2006-01-02 15:04:05"))
												}
											}
										} else {
											fmt.Printf("  ⚠️  Average: Falling behind at %.2f seconds/second\n", replicationStats.averageRatePerSecond)
										}
									}
								} else {
									fmt.Printf("%s: %s\n", field, strVal)
								}
							} else {
								fmt.Printf("%s: %s\n", field, strVal)
							}
						} else {
							fmt.Printf("%s: %s\n", field, strVal)
						}
					} else {
						fmt.Printf("%s: NULL\n", field)
					}
					break
				}
			}
		}
		fmt.Println()

		// Check for error patterns
		if lastSQLError != "" {
			for _, pattern := range errorPatterns {
				matched, err := regexp.MatchString(pattern, lastSQLError)
				if err != nil {
					log.Printf("Error matching regex pattern '%s': %v", pattern, err)
					continue
				}
				if matched {
					hasError = true
					fmt.Printf("🚨 Pattern '%s' found in Last_SQL_Error!\n", pattern)
				}
			}
		}

		return hasError
	} else {
		fmt.Printf("\n[%s] No replica status found\n", time.Now().Format("2006-01-02 15:04:05"))
		return false
	}
}
