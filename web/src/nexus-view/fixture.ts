import type { NexusViewData } from "./model";

export const decisionNexusViewFixture: NexusViewData = {
  current: {
    entityRef: "decision:dec_01JZ7RADISHNEXUS",
    entityType: "decision",
    eyebrow: "Decision · Authentication",
    title: "首期认证先采用本地账号，并为 OIDC 保留验证边界",
    status: "accepted",
    statusLabel: "已采纳",
    summary:
      "先交付可自部署、可恢复的本地账号闭环。OIDC 只在验证主体转换层预留接入点，不把未冻结的 Claim 规则扩散到应用服务。",
    projectLabel: "RadishNexus / M0 Golden Path",
    decisionOwnerLabel: "产品与架构",
    evidenceCount: 3,
    updatedAt: "2026-08-28T10:24:00+08:00",
    updatedAtLabel: "2026-08-28 10:24",
  },
  relations: [
    {
      visibility: "readable",
      entityRef: "thread:thr_01JZ7CONTEXT",
      entityType: "thread",
      entityTypeLabel: "Thread",
      relationType: "evidence_for",
      relationLabel: "讨论依据",
      title: "自部署环境的首次登录与恢复路径",
      summary: "汇总安装后首次进入、管理员恢复和实例迁移时的身份连续性约束。",
    },
    {
      visibility: "readable",
      entityRef: "ticket:tic_01JZ7IMPLEMENT",
      entityType: "ticket",
      entityTypeLabel: "Ticket",
      relationType: "implements",
      relationLabel: "实施此决策",
      title: "建立 verified user 到 Principal 的最小转换边界",
      summary:
        "传输层只接收已经验证的用户上下文，不在 application service 解析凭据。",
    },
    {
      visibility: "restricted",
    },
  ],
  timeline: [
    {
      visibility: "readable",
      id: "activity:act_01JZ7ACCEPTED",
      action: "Decision 已采纳",
      detail: "确认本地账号为首期闭环；OIDC 暂不进入公开契约。",
      actorLabel: "萝卜",
      sourceLabel: "decision.accepted",
      occurredAt: "2026-08-28T10:24:00+08:00",
      occurredAtLabel: "08-28 · 10:24",
    },
    {
      visibility: "readable",
      id: "activity:act_01JZ7TICKET",
      action: "创建实施 Ticket",
      detail:
        "把已验证主体到内部 Principal 的转换收敛为独立 transport adapter。",
      actorLabel: "萝卜",
      sourceLabel: "ticket.created",
      occurredAt: "2026-08-28T09:42:00+08:00",
      occurredAtLabel: "08-28 · 09:42",
    },
    {
      visibility: "restricted",
    },
    {
      visibility: "readable",
      id: "activity:act_01JZ7PROPOSED",
      action: "提出 Decision",
      detail:
        "从首次安装和恢复需求出发，比较本地账号与同时引入 OIDC 的边界成本。",
      actorLabel: "萝卜",
      sourceLabel: "decision.proposed",
      occurredAt: "2026-08-27T16:18:00+08:00",
      occurredAtLabel: "08-27 · 16:18",
    },
  ],
};

export const emptyDecisionNexusViewFixture: NexusViewData = {
  ...decisionNexusViewFixture,
  relations: [],
  timeline: [],
};
