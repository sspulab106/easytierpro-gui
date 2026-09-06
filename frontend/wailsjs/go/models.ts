export namespace audit {
	
	export class Entry {
	    time: string;
	    actor: string;
	    ip?: string;
	    event: string;
	    detail?: string;
	
	    static createFrom(source: any = {}) {
	        return new Entry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.time = source["time"];
	        this.actor = source["actor"];
	        this.ip = source["ip"];
	        this.event = source["event"];
	        this.detail = source["detail"];
	    }
	}

}

export namespace auth {
	
	export class Account {
	    username: string;
	    password_hash: string;
	    role: string;
	    networks?: string[];
	    created: string;
	    updated?: string;
	
	    static createFrom(source: any = {}) {
	        return new Account(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.username = source["username"];
	        this.password_hash = source["password_hash"];
	        this.role = source["role"];
	        this.networks = source["networks"];
	        this.created = source["created"];
	        this.updated = source["updated"];
	    }
	}

}

export namespace configmgr {
	
	export class ConfigFile {
	    instance_id: string;
	    instance_name: string;
	    network: string;
	    ipv4: string;
	    enabled: boolean;
	    path: string;
	    raw: string;
	
	    static createFrom(source: any = {}) {
	        return new ConfigFile(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.instance_id = source["instance_id"];
	        this.instance_name = source["instance_name"];
	        this.network = source["network"];
	        this.ipv4 = source["ipv4"];
	        this.enabled = source["enabled"];
	        this.path = source["path"];
	        this.raw = source["raw"];
	    }
	}

}

export namespace easytier {
	
	export class Version {
	    core: string;
	    cli: string;
	
	    static createFrom(source: any = {}) {
	        return new Version(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.core = source["core"];
	        this.cli = source["cli"];
	    }
	}

}

export namespace fleet {
	
	export class Agent {
	    id: string;
	    name: string;
	    os: string;
	    version: string;
	    ipv4: string;
	    networks: string[];
	    auto_start: boolean;
	    note: string;
	    // Go type: time
	    last_seen: any;
	
	    static createFrom(source: any = {}) {
	        return new Agent(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.os = source["os"];
	        this.version = source["version"];
	        this.ipv4 = source["ipv4"];
	        this.networks = source["networks"];
	        this.auto_start = source["auto_start"];
	        this.note = source["note"];
	        this.last_seen = this.convertValues(source["last_seen"], null);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Command {
	    id: string;
	    agent_id: string;
	    action: string;
	    network_id?: string;
	    network_toml?: string;
	    target?: string;
	    status: string;
	    result?: string;
	    // Go type: time
	    created: any;
	
	    static createFrom(source: any = {}) {
	        return new Command(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.agent_id = source["agent_id"];
	        this.action = source["action"];
	        this.network_id = source["network_id"];
	        this.network_toml = source["network_toml"];
	        this.target = source["target"];
	        this.status = source["status"];
	        this.result = source["result"];
	        this.created = this.convertValues(source["created"], null);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace main {
	
	export class AgentEnrollment {
	    agent: fleet.Agent;
	    token: string;
	
	    static createFrom(source: any = {}) {
	        return new AgentEnrollment(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.agent = this.convertValues(source["agent"], fleet.Agent);
	        this.token = source["token"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class PingResult {
	    host: string;
	    sent: number;
	    received: number;
	    loss_percent: number;
	    min_ms: number;
	    max_ms: number;
	    avg_ms: number;
	    jitter_ms: number;
	    rtts: number[];
	
	    static createFrom(source: any = {}) {
	        return new PingResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.host = source["host"];
	        this.sent = source["sent"];
	        this.received = source["received"];
	        this.loss_percent = source["loss_percent"];
	        this.min_ms = source["min_ms"];
	        this.max_ms = source["max_ms"];
	        this.avg_ms = source["avg_ms"];
	        this.jitter_ms = source["jitter_ms"];
	        this.rtts = source["rtts"];
	    }
	}

}

export namespace traffic {
	
	export class DayTotal {
	    date: string;
	    rx: number;
	    tx: number;
	
	    static createFrom(source: any = {}) {
	        return new DayTotal(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.date = source["date"];
	        this.rx = source["rx"];
	        this.tx = source["tx"];
	    }
	}
	export class Point {
	    ts: number;
	    rx: number;
	    tx: number;
	
	    static createFrom(source: any = {}) {
	        return new Point(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ts = source["ts"];
	        this.rx = source["rx"];
	        this.tx = source["tx"];
	    }
	}
	export class History {
	    hours_24: Point[];
	    days: DayTotal[];
	    today_rx: number;
	    today_tx: number;
	    week_rx: number;
	    week_tx: number;
	
	    static createFrom(source: any = {}) {
	        return new History(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.hours_24 = this.convertValues(source["hours_24"], Point);
	        this.days = this.convertValues(source["days"], DayTotal);
	        this.today_rx = source["today_rx"];
	        this.today_tx = source["today_tx"];
	        this.week_rx = source["week_rx"];
	        this.week_tx = source["week_tx"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace tunnel {
	
	export class Peer {
	    network: string;
	    hostname: string;
	    ip: string;
	
	    static createFrom(source: any = {}) {
	        return new Peer(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.network = source["network"];
	        this.hostname = source["hostname"];
	        this.ip = source["ip"];
	    }
	}
	export class Tunnel {
	    id: string;
	    kind: string;
	    target: string;
	    public_url: string;
	    status: string;
	    error?: string;
	    // Go type: time
	    created: any;
	
	    static createFrom(source: any = {}) {
	        return new Tunnel(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.kind = source["kind"];
	        this.target = source["target"];
	        this.public_url = source["public_url"];
	        this.status = source["status"];
	        this.error = source["error"];
	        this.created = this.convertValues(source["created"], null);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

