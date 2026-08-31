package invocation

type Final struct {
	Executable       string
	Argv             []string
	CWD              string
	Environment      []string
	TargetExecutable string
	TargetArgv       []string
	BindingPayload   string
	BridgePath       string
	BridgeCreated    bool
}

type Resolver struct {
	environment []string
	path        string
	systemRoot  string
}
