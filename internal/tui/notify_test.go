package tui

import (
	"strings"
	"testing"

	"github.com/aeon022/missionctl-core/applescript"
)

// A task title like  x" & (do shell script "id") & "  must stay inside the string literal.
func TestNotificationTitleCannotBreakOutOfTheLiteral(t *testing.T) {
	evil := `x" & (do shell script "id") & "`
	lit := applescript.EscapeLine("Done: " + evil)
	if strings.Contains(strings.ReplaceAll(lit, `\"`, ""), `"`) {
		t.Fatalf("an unescaped quote survived: %s", lit)
	}
}
