package processruntime

import (
	"reflect"
	"testing"

	"github.com/AAAYNMMM/CWapi/internal/invocation"
)

func TestPolicyInvocationUsesDirectBatchTarget(t *testing.T) {
	final := invocation.Final{
		Executable:       `C:\Windows\System32\cmd.exe`,
		Argv:             []string{"/d", "/c", ".cwapi-process-bridge.cmd", "binding"},
		CWD:              `C:\repo`,
		TargetExecutable: `C:\tools\git.cmd`,
		TargetArgv:       []string{"commit", "-m", "message"},
	}
	policy := policyInvocation(final)
	if policy.Executable != final.TargetExecutable || !reflect.DeepEqual(policy.Argv, final.TargetArgv) || policy.CWD != final.CWD {
		t.Fatalf("policy invocation = %#v", policy)
	}
}

func TestPolicyInvocationUsesNativeFinal(t *testing.T) {
	final := invocation.Final{Executable: `C:\tools\node.exe`, Argv: []string{"script.js"}, CWD: `C:\repo`}
	policy := policyInvocation(final)
	if policy.Executable != final.Executable || !reflect.DeepEqual(policy.Argv, final.Argv) || policy.CWD != final.CWD {
		t.Fatalf("policy invocation = %#v", policy)
	}
}
