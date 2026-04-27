# RASA Database Bootstrap
# Run from project root. Requires psql >= 16 and superuser.
# Usage: .\scripts\create_databases.ps1 -Password "8764"

param(
    [string]$Server = "localhost",
    [string]$Port = "5432",
    [string]$User = "postgres",
    [string]$Password = "8764"
)

$ErrorActionPreference = "Stop"

# Set PGPASSWORD
$env:PGPASSWORD = $Password

$Databases = @(
    "rasa_orch",
    "rasa_pool",
    "rasa_policy",
    "rasa_memory",
    "rasa_eval",
    "rasa_recovery"
)

# Create each database (idempotent)
foreach ($db in $Databases) {
    $exists = psql -h $Server -p $Port -U $User -tc "SELECT 1 FROM pg_database WHERE datname='$db';" | Out-String
    if ($exists.Trim() -ne "1") {
        Write-Host "Creating database: $db"
        psql -h $Server -p $Port -U $User -tc "CREATE DATABASE $db;"
    } else {
        Write-Host "Database exists: $db"
    }
}

Write-Host "Done.`nNext: run bootstrap_schema.ps1 to create tables and extensions."
