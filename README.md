# MySQL Replica Monitor

A Go program that connects to a MySQL database and continuously monitors replica status by executing `SHOW REPLICA STATUS` commands.

## Features

- Connects to MySQL database using provided credentials
- Executes `SHOW REPLICA STATUS` every 5 seconds
- Displays key replica status fields including:
  - Replica_IO_State
  - Source_Host
  - Source_Port
  - Replica_IO_Running
  - Replica_SQL_Running
  - Replicate_Do_DB
  - Replicate_Ignore_DB
  - Last_IO_Error
  - Last_SQL_Error
  - Seconds_Behind_Source

## Prerequisites

- Go 1.21 or later
- Network access to the MySQL database

## Installation

1. Clone or download this repository
2. Navigate to the project directory
3. Install dependencies:
   ```bash
   go mod tidy
   ```

## Usage

Run the program with required database parameters:
```bash
go run main.go -host <hostname> -user <username> -password <password>
```

Or build and run:
```bash
go build -o replica-monitor main.go
./replica-monitor -host <hostname> -user <username> -password <password>
```

The program will:
1. Connect to the MySQL database
2. Start monitoring replica status every 5 seconds
3. Display the results to the console
4. Continue until interrupted with Ctrl+C

## Configuration

The database connection details are provided via command line arguments:

### Required Parameters:
- `-host`: MySQL hostname
- `-user`: MySQL username  
- `-password`: MySQL password

### Optional Parameters:
- `-port`: MySQL port (default: 3306)

## Output Example

```
Successfully connected to MySQL database
Starting replica status monitoring...
Press Ctrl+C to stop

[2025-07-24 16:10:46] Replica Status:
==================================================
Replica_IO_State: Waiting for the replica SQL thread to free relay log space
Source_Host: 10.1.15.0
Source_Port: 3306
Replica_IO_Running: Yes
Replica_SQL_Running: Yes
Replicate_Do_DB:
Replicate_Ignore_DB:
Last_IO_Error:
Last_SQL_Error:
Seconds_Behind_Source: 83d 5h 35m 16s
📊 Replication Performance:
  🚀 Instant: Catching up at 48.40 seconds/second
  ⏰ Instant ETA: 1d 17h 16m 9s (2025-07-26 09:26:55)
  📈 Average: Catching up at 45.01 seconds/second
  ⏰ Average ETA: 1d 20h 22m 39s (2025-07-26 12:33:25)

``` 
