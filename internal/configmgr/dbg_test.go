package configmgr

import (
	"fmt"
	"testing"

	"github.com/BurntSushi/toml"
)

func TestDbgInject(t *testing.T) {
	out := injectDevName(sampleToml, "11111111-2222-3333-4444-555555555555")
	fmt.Printf("INJECTED:\n%s\n====\n", out)
	var m meta
	if err2 := toml.Unmarshal([]byte(out), &m); err2 != nil {
		t.Fatalf("unmarshal error: %v", err2)
	}
	fmt.Printf("meta: %+v\n", m)
}
