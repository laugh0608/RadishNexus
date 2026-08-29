export type NexusEntityType = "decision" | "thread" | "ticket";

export interface NexusCurrent {
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
