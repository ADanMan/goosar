// Производные типы присутствия агентов — состояние, которое показываем пользователю.

export type AgentAvailability =
  | 'online' 
  | 'unstable' 
  | 'offline' 
  | 'archived'; 

export type Workload =
  | 'working' 
  | 'queued' 
  | 'idle'; 

export interface AgentPresenceDetail {
  availability: AgentAvailability;
  workload: Workload;
  runningCount: number;
  queuedCount: number;
  capacity: number;
}
