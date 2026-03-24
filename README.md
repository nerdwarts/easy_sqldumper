# Easy SQLdumper

A minimal CLI tool that wraps `mysqldump` to create timestamped SQL backups of MySQL/MariaDB databases, configured via a TOML file.

## Features

- 📦 Timestamped backup files (`dbname_2026-03-24_15-04-05.sql`)
- 🔐 Secure password handling via `--defaults-extra-file` (not visible in `ps aux`)
- 🔒 Optional SSL/TLS support
- ⚙️ Simple TOML configuration
- 🧹 Automatic cleanup of partial backups on failure

## Requirements

- Go 1.22+
- `mysqldump` (MySQL or MariaDB client) available in `$PATH`

## Installation

```bash
git clone https://github.com/youruser/sqldumper.git
cd sqldumper
go build -o sqldumper .
```

## Configuration

Copy and edit the example config file:

```bash
cp dumper.toml my-config.toml
```

```toml
[database]
user     = "your_user"
password = "your_password"
host     = "127.0.0.1"
port     = 3306        # optional, defaults to 3306

[ssl]
enabled             = false
ca                  = "/path/to/ca-cert.pem"
cert                = "/path/to/client-cert.pem"
key                 = "/path/to/client-key.pem"
verify_server_cert  = true
```

## Usage

```bash
# Minimal — uses ./dumper.toml and saves to ./backup/
./sqldumper -db my_database

# Custom config and output directory
./sqldumper -db my_database -config /etc/sqldumper.toml -dir /var/backups/mysql
```

### Flags

| Flag      | Default          | Description                        |
|-----------|------------------|------------------------------------|
| `-db`     | *(required)*     | Name of the database to back up    |
| `-dir`    | `./backup`       | Directory to save the backup file  |
| `-config` | `./dumper.toml`  | Path to the TOML configuration file|

## Output

Backups are saved as:

```
<dir>/<dbname>_<YYYY-MM-DD_HH-MM-SS>.sql
```

Example: `./backup/my_database_2026-03-24_15-04-05.sql`

## Security

Passwords are never passed as CLI arguments. Instead, sqldumper writes the password to a temporary file (`sqldumper-*.cnf`) and passes it to `mysqldump` via `--defaults-extra-file`. The file is deleted immediately after the dump completes.

## License

MIT

