# Inventory pipeline

Pipeline atual:
1. `tailscale status --json` (quando `tailscale` + `jq` existem)
2. hosts salvos (`~/.pocketcli/hosts`)
3. seeds (`~/.pocketcli/fallback_seeds`)
4. reconciliação por hostname
5. persistência em `~/.local/share/pocketcli/inventory.json`

Aprovação/trust ficam desacoplados de online/reachable.
