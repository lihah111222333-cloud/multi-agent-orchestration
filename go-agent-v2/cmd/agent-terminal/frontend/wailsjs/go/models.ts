export namespace runner {
	
	export class AgentInfo {
	    id: string;
	    name: string;
	    port: number;
	    thread_id: string;
	    state: string;
	
	    static createFrom(source: any = {}) {
	        return new AgentInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.port = source["port"];
	        this.thread_id = source["thread_id"];
	        this.state = source["state"];
	    }
	}

}

