// 1. get session
// 2. if session is null, start a new one and wait for it to be ready
// 3. if session is not null, connect to it

export class WDAClient {
    private session: Session;

    constructor(public wdaURL: string) {
        this.session = new Session(wdaURL);
    }
    
    async getSession(): Promise<Session> {
        await this.session.ensureSessionId();
        return this.session;
    }
}

export class Session {
    constructor(public wdaURL: string, public sessionId?: string) { }

    async ensureSessionId(): Promise<string> {
        if (this.sessionId) {
            return this.sessionId;
        }
        const currentSessionId = await this.getSessionId();
        if (currentSessionId) {
            this.sessionId = currentSessionId;
            return this.sessionId;
        }
        this.sessionId = await this.createSessionId();
        return this.sessionId;
    }

    async getSessionId(): Promise<string | null> {
        const response = await fetch(`${this.wdaURL}/status`);
        const data: { sessionId: string | null} = await response.json();
        return data.sessionId;
    }

    async createSessionId(): Promise<string> {
        const response = await fetch(`${this.wdaURL}/session`, {
            method: "POST",
            headers: {
                "Content-Type": "application/json",
            },
            body: JSON.stringify({
                capabilities: {},
            }),
        });
        const data: { sessionId: string } = await response.json();
        return data.sessionId;
    }
}
