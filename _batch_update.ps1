# === Batch 1: Simple transport string replacements ===

$docs = @{
  'C:\Users\goldf\Rasa\schema\implementation\policy_engine.md' = @{
    'NATS JetStream' = 'Redis Pub/Sub';
    'NATS `policy.update`' = 'Redis `policy.update`';
    'NATS consumer' = 'Redis subscriber';
    'NATS, local NATS' = 'Redis';
    'NATS, local NATS, local LLM Gateway' = 'Redis, local LLM Gateway';
    'nats localhost:4222' = 'redis localhost:6379';
    '--nats localhost:4222' = '';
    'Transport for live updates' = 'Transport for live updates';
  }
}

foreach ($path in $docs.Keys) {
  $content = Get-Content $path -Raw
  foreach ($old in $docs[$path].Keys) {
    $new = $docs[$path][$old]
    $content = $content -replace [regex]::Escape($old), $new
  }
  Set-Content $path -Value $content -Encoding UTF8
  Write-Host "Updated: $path"
}
