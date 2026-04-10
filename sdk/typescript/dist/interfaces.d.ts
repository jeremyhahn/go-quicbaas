import type { AgentBackendInfo, AgentRegistration, HealthCheckReport, JobEvent, JobStatus, SessionInfo, StartJobRequest, StartJobResponse, Task } from './types.js';
export interface ConnectionClient {
    connect(address: string, transport?: string, tenantID?: string): Promise<void>;
    disconnect(): Promise<void>;
    isConnected(): boolean;
    getAddress(): string;
    getTransport(): string;
}
export interface OrchestratorClient {
    listJobs(tenantID: string): Promise<JobStatus[]>;
    getJobStatus(tenantID: string, jobID: string): Promise<JobStatus>;
    startJob(req: StartJobRequest): Promise<StartJobResponse>;
    cancelJob(tenantID: string, jobID: string): Promise<void>;
    listAgents(tenantID: string): Promise<AgentRegistration[]>;
    getAgent(tenantID: string, agentID: string): Promise<AgentRegistration>;
    listSessions(backend?: string): Promise<SessionInfo[]>;
    listTasks(tenantID: string): Promise<Task[]>;
    getTask(tenantID: string, taskID: string): Promise<Task>;
    listBackends(): Promise<AgentBackendInfo[]>;
    healthCheck(): Promise<HealthCheckReport>;
    subscribeJobEvents(tenantID: string, jobID: string, onEvent: (event: JobEvent) => void): () => void;
}
export interface Client {
    readonly connection: ConnectionClient;
    readonly orchestrator: OrchestratorClient;
}
//# sourceMappingURL=interfaces.d.ts.map