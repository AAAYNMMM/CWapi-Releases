export namespace app {
	export class ComponentSnapshot {
		name: string;
		state: string;
		detail: string;
		updated_at: number;
		static createFrom(source: any = {}) { return new ComponentSnapshot(source); }
		constructor(source: any = {}) {
			if ('string' === typeof source) source = JSON.parse(source);
			this.name = source["name"];
			this.state = source["state"];
			this.detail = source["detail"];
			this.updated_at = source["updated_at"];
		}
	}

	export class ProjectSnapshot {
		id: string;
		display_name: string;
		repository: string;
		local_path: string;
		remote_url: string;
		static createFrom(source: any = {}) { return new ProjectSnapshot(source); }
		constructor(source: any = {}) {
			if ('string' === typeof source) source = JSON.parse(source);
			this.id = source["id"];
			this.display_name = source["display_name"];
			this.repository = source["repository"];
			this.local_path = source["local_path"];
			this.remote_url = source["remote_url"];
		}
	}

	export class SlackConfigSnapshot {
		channel_id: string;
		static createFrom(source: any = {}) { return new SlackConfigSnapshot(source); }
		constructor(source: any = {}) {
			if ('string' === typeof source) source = JSON.parse(source);
			this.channel_id = source["channel_id"];
		}
	}

	export class ConfigSnapshot {
		schema: string;
		version: string;
		config_path: string;
		permission_mode: string;
		slack: SlackConfigSnapshot;
		projects: ProjectSnapshot[];
		static createFrom(source: any = {}) { return new ConfigSnapshot(source); }
		constructor(source: any = {}) {
			if ('string' === typeof source) source = JSON.parse(source);
			this.schema = source["schema"];
			this.version = source["version"];
			this.config_path = source["config_path"];
			this.permission_mode = source["permission_mode"];
			this.slack = this.convertValues(source["slack"], SlackConfigSnapshot);
			this.projects = this.convertValues(source["projects"], ProjectSnapshot);
		}
		convertValues(a: any, classs: any, asMap: boolean = false): any {
			if (!a) return a;
			if (a.slice && a.map) return (a as any[]).map(elem => this.convertValues(elem, classs));
			if ("object" === typeof a) {
				if (asMap) { for (const key of Object.keys(a)) a[key] = new classs(a[key]); return a; }
				return new classs(a);
			}
			return a;
		}
	}

	export class ConfigureSlackCommand {
		app_token: string;
		bot_token: string;
		channel_id: string;
		static createFrom(source: any = {}) { return new ConfigureSlackCommand(source); }
		constructor(source: any = {}) {
			if ('string' === typeof source) source = JSON.parse(source);
			this.app_token = source["app_token"];
			this.bot_token = source["bot_token"];
			this.channel_id = source["channel_id"];
		}
	}

	export class ErrorSnapshot {
		fingerprint: string;
		component: string;
		operation: string;
		message: string;
		count: number;
		first_seen: number;
		last_seen: number;
		active: boolean;
		static createFrom(source: any = {}) { return new ErrorSnapshot(source); }
		constructor(source: any = {}) {
			if ('string' === typeof source) source = JSON.parse(source);
			this.fingerprint = source["fingerprint"];
			this.component = source["component"];
			this.operation = source["operation"];
			this.message = source["message"];
			this.count = source["count"];
			this.first_seen = source["first_seen"];
			this.last_seen = source["last_seen"];
			this.active = source["active"];
		}
	}

	export class RuntimeLogSnapshot {
		id: number;
		timestamp: number;
		level: string;
		component: string;
		message: string;
		fields_json: string;
		fingerprint: string;
		static createFrom(source: any = {}) { return new RuntimeLogSnapshot(source); }
		constructor(source: any = {}) {
			if ('string' === typeof source) source = JSON.parse(source);
			this.id = source["id"];
			this.timestamp = source["timestamp"];
			this.level = source["level"];
			this.component = source["component"];
			this.message = source["message"];
			this.fields_json = source["fields_json"];
			this.fingerprint = source["fingerprint"];
		}
	}

	export class ExecutionEventSnapshot {
		id: number;
		timestamp: number;
		task_id: string;
		step_id: string;
		kind: string;
		status: string;
		message: string;
		duration_ms: number;
		data_json: string;
		static createFrom(source: any = {}) { return new ExecutionEventSnapshot(source); }
		constructor(source: any = {}) {
			if ('string' === typeof source) source = JSON.parse(source);
			this.id = source["id"];
			this.timestamp = source["timestamp"];
			this.task_id = source["task_id"];
			this.step_id = source["step_id"];
			this.kind = source["kind"];
			this.status = source["status"];
			this.message = source["message"];
			this.duration_ms = source["duration_ms"];
			this.data_json = source["data_json"];
		}
	}

	export class ObservabilitySnapshot {
		state_path: string;
		state_schema: string;
		structured_execution: ExecutionEventSnapshot[];
		runtime_logs: RuntimeLogSnapshot[];
		errors: ErrorSnapshot[];
		components: ComponentSnapshot[];
		static createFrom(source: any = {}) { return new ObservabilitySnapshot(source); }
		constructor(source: any = {}) {
			if ('string' === typeof source) source = JSON.parse(source);
			this.state_path = source["state_path"];
			this.state_schema = source["state_schema"];
			this.structured_execution = this.convertValues(source["structured_execution"], ExecutionEventSnapshot);
			this.runtime_logs = this.convertValues(source["runtime_logs"], RuntimeLogSnapshot);
			this.errors = this.convertValues(source["errors"], ErrorSnapshot);
			this.components = this.convertValues(source["components"], ComponentSnapshot);
		}
		convertValues(a: any, classs: any, asMap: boolean = false): any {
			if (!a) return a;
			if (a.slice && a.map) return (a as any[]).map(elem => this.convertValues(elem, classs));
			if ("object" === typeof a) {
				if (asMap) { for (const key of Object.keys(a)) a[key] = new classs(a[key]); return a; }
				return new classs(a);
			}
			return a;
		}
	}

	export class SlackSnapshot {
		configured: boolean;
		ready: boolean;
		state: string;
		detail: string;
		credential_store: string;
		app_token_present: boolean;
		bot_token_present: boolean;
		channel_id: string;
		channel_name: string;
		team: string;
		team_id: string;
		user: string;
		user_id: string;
		bot_id: string;
		socket_ready: boolean;
		recent_index_size: number;
		static createFrom(source: any = {}) { return new SlackSnapshot(source); }
		constructor(source: any = {}) {
			if ('string' === typeof source) source = JSON.parse(source);
			this.configured = source["configured"];
			this.ready = source["ready"];
			this.state = source["state"];
			this.detail = source["detail"];
			this.credential_store = source["credential_store"];
			this.app_token_present = source["app_token_present"];
			this.bot_token_present = source["bot_token_present"];
			this.channel_id = source["channel_id"];
			this.channel_name = source["channel_name"];
			this.team = source["team"];
			this.team_id = source["team_id"];
			this.user = source["user"];
			this.user_id = source["user_id"];
			this.bot_id = source["bot_id"];
			this.socket_ready = source["socket_ready"];
			this.recent_index_size = source["recent_index_size"];
		}
	}

	export class RuntimeSnapshot {
		version: string;
		source_commit: string;
		architecture: string;
		core: string;
		desktop: string;
		platform: string;
		stage: string;
		static createFrom(source: any = {}) { return new RuntimeSnapshot(source); }
		constructor(source: any = {}) {
			if ('string' === typeof source) source = JSON.parse(source);
			this.version = source["version"];
			this.source_commit = source["source_commit"];
			this.architecture = source["architecture"];
			this.core = source["core"];
			this.desktop = source["desktop"];
			this.platform = source["platform"];
			this.stage = source["stage"];
		}
	}

	export class MCPRequestSnapshot {
		request_id: string;
		source_message_id: string;
		method: string;
		tool_name: string;
		execution_state: string;
		delivery_state: string;
		terminal: boolean;
		created_at: number;
		updated_at: number;
		elapsed_ms: number;
		static createFrom(source: any = {}) { return new MCPRequestSnapshot(source); }
		constructor(source: any = {}) {
			if ('string' === typeof source) source = JSON.parse(source);
			this.request_id = source["request_id"];
			this.source_message_id = source["source_message_id"];
			this.method = source["method"];
			this.tool_name = source["tool_name"];
			this.execution_state = source["execution_state"];
			this.delivery_state = source["delivery_state"];
			this.terminal = source["terminal"];
			this.created_at = source["created_at"];
			this.updated_at = source["updated_at"];
			this.elapsed_ms = source["elapsed_ms"];
		}
	}

	export class CodexSnapshot {
		configured: boolean;
		ready: boolean;
		running: boolean;
		version: string;
		executable_path: string;
		executable_sha256: string;
		browser_mcp_ready: boolean;
		process_mcp_ready: boolean;
		node_path: string;
		browser_path: string;
		static createFrom(source: any = {}) { return new CodexSnapshot(source); }
		constructor(source: any = {}) {
			if ('string' === typeof source) source = JSON.parse(source);
			this.configured = source["configured"];
			this.ready = source["ready"];
			this.running = source["running"];
			this.version = source["version"];
			this.executable_path = source["executable_path"];
			this.executable_sha256 = source["executable_sha256"];
			this.browser_mcp_ready = source["browser_mcp_ready"];
			this.process_mcp_ready = source["process_mcp_ready"];
			this.node_path = source["node_path"];
			this.browser_path = source["browser_path"];
		}
	}

	export class DesktopSnapshot {
		generated_at: number;
		runtime: RuntimeSnapshot;
		config: ConfigSnapshot;
		slack: SlackSnapshot;
		codex: CodexSnapshot;
		mcp_requests: MCPRequestSnapshot[];
		observability: ObservabilitySnapshot;
		static createFrom(source: any = {}) { return new DesktopSnapshot(source); }
		constructor(source: any = {}) {
			if ('string' === typeof source) source = JSON.parse(source);
			this.generated_at = source["generated_at"];
			this.runtime = this.convertValues(source["runtime"], RuntimeSnapshot);
			this.config = this.convertValues(source["config"], ConfigSnapshot);
			this.slack = this.convertValues(source["slack"], SlackSnapshot);
			this.codex = this.convertValues(source["codex"], CodexSnapshot);
			this.mcp_requests = this.convertValues(source["mcp_requests"], MCPRequestSnapshot);
			this.observability = this.convertValues(source["observability"], ObservabilitySnapshot);
		}
		convertValues(a: any, classs: any, asMap: boolean = false): any {
			if (!a) return a;
			if (a.slice && a.map) return (a as any[]).map(elem => this.convertValues(elem, classs));
			if ("object" === typeof a) {
				if (asMap) { for (const key of Object.keys(a)) a[key] = new classs(a[key]); return a; }
				return new classs(a);
			}
			return a;
		}
	}

	export class DiagnosticsSnapshot {
		generated_at: number;
		version: string;
		source_commit: string;
		architecture: string;
		platform: string;
		stage: string;
		config_path: string;
		state_path: string;
		state_schema: string;
		slack: SlackSnapshot;
		codex: CodexSnapshot;
		mcp_requests: MCPRequestSnapshot[];
		components: ComponentSnapshot[];
		static createFrom(source: any = {}) { return new DiagnosticsSnapshot(source); }
		constructor(source: any = {}) {
			if ('string' === typeof source) source = JSON.parse(source);
			this.generated_at = source["generated_at"];
			this.version = source["version"];
			this.source_commit = source["source_commit"];
			this.architecture = source["architecture"];
			this.platform = source["platform"];
			this.stage = source["stage"];
			this.config_path = source["config_path"];
			this.state_path = source["state_path"];
			this.state_schema = source["state_schema"];
			this.slack = this.convertValues(source["slack"], SlackSnapshot);
			this.codex = this.convertValues(source["codex"], CodexSnapshot);
			this.mcp_requests = this.convertValues(source["mcp_requests"], MCPRequestSnapshot);
			this.components = this.convertValues(source["components"], ComponentSnapshot);
		}
		convertValues(a: any, classs: any, asMap: boolean = false): any {
			if (!a) return a;
			if (a.slice && a.map) return (a as any[]).map(elem => this.convertValues(elem, classs));
			if ("object" === typeof a) {
				if (asMap) { for (const key of Object.keys(a)) a[key] = new classs(a[key]); return a; }
				return new classs(a);
			}
			return a;
		}
	}

	export class ReadinessSnapshot {
		generated_at: number;
		runtime: RuntimeSnapshot;
		slack: SlackSnapshot;
		codex: CodexSnapshot;
		mcp_runtime_ready: boolean;
		local_ready: boolean;
		ready: boolean;
		detail: string;
		recent_requests: MCPRequestSnapshot[];
		static createFrom(source: any = {}) { return new ReadinessSnapshot(source); }
		constructor(source: any = {}) {
			if ('string' === typeof source) source = JSON.parse(source);
			this.generated_at = source["generated_at"];
			this.runtime = this.convertValues(source["runtime"], RuntimeSnapshot);
			this.slack = this.convertValues(source["slack"], SlackSnapshot);
			this.codex = this.convertValues(source["codex"], CodexSnapshot);
			this.mcp_runtime_ready = source["mcp_runtime_ready"];
			this.local_ready = source["local_ready"];
			this.ready = source["ready"];
			this.detail = source["detail"];
			this.recent_requests = this.convertValues(source["recent_requests"], MCPRequestSnapshot);
		}
		convertValues(a: any, classs: any, asMap: boolean = false): any {
			if (!a) return a;
			if (a.slice && a.map) return (a as any[]).map(elem => this.convertValues(elem, classs));
			if ("object" === typeof a) {
				if (asMap) { for (const key of Object.keys(a)) a[key] = new classs(a[key]); return a; }
				return new classs(a);
			}
			return a;
		}
	}

	export class ProjectCommand {
		display_name: string;
		repository: string;
		local_path: string;
		remote_url: string;
		static createFrom(source: any = {}) { return new ProjectCommand(source); }
		constructor(source: any = {}) {
			if ('string' === typeof source) source = JSON.parse(source);
			this.display_name = source["display_name"];
			this.repository = source["repository"];
			this.local_path = source["local_path"];
			this.remote_url = source["remote_url"];
		}
	}

	export class SlackMessageSnapshot {
		message_id: string;
		channel_id: string;
		message_ts: string;
		thread_ts?: string;
		subject: string;
		body: string;
		bot_id?: string;
		user_id?: string;
		static createFrom(source: any = {}) { return new SlackMessageSnapshot(source); }
		constructor(source: any = {}) {
			if ('string' === typeof source) source = JSON.parse(source);
			this.message_id = source["message_id"];
			this.channel_id = source["channel_id"];
			this.message_ts = source["message_ts"];
			this.thread_ts = source["thread_ts"];
			this.subject = source["subject"];
			this.body = source["body"];
			this.bot_id = source["bot_id"];
			this.user_id = source["user_id"];
		}
	}
}
