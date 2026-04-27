# RASA Schema Bootstrap
# Creates all tables, indexes, and extensions across 6 databases.
# Must run create_databases.ps1 first.
#
# Usage: .\scripts\bootstrap_schema.ps1 -Password "8764"

param(
    [string]$Server = "localhost",
    [string]$Port = "5432",
    [string]$User = "postgres",
    [string]$Password = "8764"
)

$ErrorActionPreference = "Stop"
$env:PGPASSWORD = $Password

$Migrations = @(
    "migrations/001_enables.sql",
    "migrations/010_rasa_orch.sql",
    "migrations/020_rasa_pool.sql",
    "migrations/030_rasa_policy.sql",
    "migrations/040_rasa_memory.sql",
    "migrations/050_rasa_eval.sql",
    "migrations/060_rasa_recovery.sql"
)

foreach ($file in $Migrations) {
    if (Test-Path $file) {
        Write-Host "Applying $file ..."
        psql -h $Server -p $Port -U $User -f $file
    } else {
        Write-Warning "Missing: $file"
    }
}

Write-Host "Schema bootstrap complete."
