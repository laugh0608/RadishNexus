export type NexusEntityType =
  "decision" | "thread" | "ticket" | "ci-run" | "environment";

export interface DecisionNexusCurrent {
  entityRef: string;
  entityType: "decision";
  eyebrow: string;
  title: string;
  status: "accepted" | "proposed";
  statusLabel: string;
  summary: string;
  projectLabel: string;
  decisionOwnerLabel: string;
  evidenceCount: number;
  updatedAt: string;
  updatedAtLabel: string;
}

export type CIRunStatus = "succeeded" | "failed" | "canceled";

export interface CIRunComponent {
  entityRef: string;
  name: string;
}

export interface CIRunNexusCurrent {
  entityRef: string;
  entityType: "ci-run";
  eyebrow: string;
  status: CIRunStatus;
  statusLabel: string;
  summary: string;
  component: CIRunComponent;
  startedAt: string;
  startedAtLabel: string;
  completedAt: string;
  completedAtLabel: string;
  recordedAt: string;
  recordedAtLabel: string;
  updatedAt: string;
  updatedAtLabel: string;
}

export type DeploymentStatus = "succeeded" | "failed" | "canceled";

export interface DeploymentSubject {
  entityRef: string;
  name: string;
}

export interface DeploymentNexusCurrent {
  entityRef: string;
  entityType: "deployment";
  eyebrow: string;
  status: DeploymentStatus;
  statusLabel: string;
  summary: string;
  environment: DeploymentSubject;
  ciRun: DeploymentSubject;
  startedAt: string | null;
  startedAtLabel: string;
  completedAt: string;
  completedAtLabel: string;
  recordedAt: string;
  recordedAtLabel: string;
}

export type NexusCurrent =
  DecisionNexusCurrent | CIRunNexusCurrent | DeploymentNexusCurrent;

export interface ReadableNexusRelation {
  visibility: "readable";
  entityRef: string;
  entityType: NexusEntityType;
  entityTypeLabel: string;
  relationType: string;
  relationLabel: string;
  title: string;
  summary: string;
}

export interface RestrictedNexusItem {
  visibility: "restricted";
}

export type NexusRelation = ReadableNexusRelation | RestrictedNexusItem;

export interface ReadableTimelineItem {
  visibility: "readable";
  id: string;
  action: string;
  detail: string;
  actorLabel: string;
  sourceLabel: string;
  occurredAt: string;
  occurredAtLabel: string;
}

export type NexusTimelineItem = ReadableTimelineItem | RestrictedNexusItem;

export interface NexusViewData {
  current: NexusCurrent;
  relations: readonly NexusRelation[];
  timeline: readonly NexusTimelineItem[];
}

export type NexusViewState =
  | { status: "loading" }
  | { status: "error"; message: string }
  | { status: "ready"; data: NexusViewData };
