#!/usr/bin/env bash
set -e

BINARY="taskctl"
INSTALL_DIR="$HOME/.local/bin"
CLAUDE_JSON="$HOME/.claude.json"

echo "taskctl setup"
echo "─────────────────────────────────────"

# ── Go check ──────────────────────────────────────────────────────────────────
if ! command -v go &>/dev/null; then
  echo "Error: Go not found. Install with: brew install go"
  exit 1
fi
echo "✓ Go $(go version | awk '{print $3}')"

# ── Build ─────────────────────────────────────────────────────────────────────
echo "  Building…"
go build -o "$BINARY" .
echo "✓ Built"

# ── Install binary ────────────────────────────────────────────────────────────
mkdir -p "$INSTALL_DIR"
cp "$BINARY" "$INSTALL_DIR/$BINARY"
chmod +x "$INSTALL_DIR/$BINARY"
echo "✓ Installed to $INSTALL_DIR/$BINARY"

# ── PATH check ────────────────────────────────────────────────────────────────
if ! echo "$PATH" | grep -q "$INSTALL_DIR"; then
  echo ""
  echo "  Add to your shell config (~/.zshrc or ~/.bash_profile):"
  echo "  export PATH=\"\$HOME/.local/bin:\$PATH\""
  echo ""
fi

# ── MCP: register in ~/.claude.json ───────────────────────────────────────────
if command -v python3 &>/dev/null; then
  python3 - "$CLAUDE_JSON" "$INSTALL_DIR/$BINARY" <<'PYEOF'
import json, sys, os

claude_json = sys.argv[1]
binary_path = sys.argv[2]

data = {}
if os.path.exists(claude_json):
    with open(claude_json) as f:
        try:
            data = json.load(f)
        except Exception:
            pass

data.setdefault("mcpServers", {})
data["mcpServers"]["taskctl"] = {
    "command": binary_path,
    "args": ["mcp"]
}

with open(claude_json, "w") as f:
    json.dump(data, f, indent=2)
    f.write("\n")

print("✓ MCP server registered in ~/.claude.json")
print("  Restart Claude Code to activate taskctl MCP tools")
PYEOF
else
  echo ""
  echo "  To enable MCP (Claude Code integration), add to ~/.claude.json:"
  echo "  \"mcpServers\": { \"taskctl\": { \"command\": \"$INSTALL_DIR/$BINARY\", \"args\": [\"mcp\"] } }"
fi

# ── First sync reminder ───────────────────────────────────────────────────────
echo ""
echo "─────────────────────────────────────"
echo "Done. Run:"
echo "  taskctl sync    # first sync (macOS will ask for Reminders access)"
echo "  taskctl         # open TUI"
