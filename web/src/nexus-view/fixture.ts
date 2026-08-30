import type { NexusViewData } from "./model";

export const decisionNexusViewFixture: NexusViewData = {
  current: {
    entityRef: "entity://decision/dec_01JZ7RADISHNEXUS",
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
      entityRef: "entity://thread/thr_01JZ7CONTEXT",
      entityType: "thread",
      entityTypeLabel: "Thread",
      relationType: "evidence_for",
      relationLabel: "讨论依据",
      title: "自部署环境的首次登录与恢复路径",
      summary: "汇总安装后首次进入、管理员恢复和实例迁移时的身份连续性约束。",
    },
    {
      visibility: "readable",
      entityRef: "entity://ticket/tkt_01JZ7IMPLEMENT",
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

export const succeededCIRunNexusViewFixture = {
  current: {
    entityRef: "entity://ci-run/cir_01K3RADISHNEXUS",
    entityType: "ci-run",
    eyebrow: "CI Run · Completed build fact",
    status: "succeeded",
    statusLabel: "构建成功",
    summary:
      "一次构建流水线已经完成，并被记录为成功。此事实只说明构建结果，不表示任何 Environment 已经部署。",
    component: {
      entityRef: "entity://component/cmp_identity",
      name: "Identity Service",
    },
    startedAt: "2026-08-29T09:12:18+08:00",
    startedAtLabel: "2026-08-29 09:12:18",
    completedAt: "2026-08-29T09:18:42+08:00",
    completedAtLabel: "2026-08-29 09:18:42",
    recordedAt: "2026-08-29T09:18:44+08:00",
    recordedAtLabel: "2026-08-29 09:18:44",
    updatedAt: "2026-08-29T09:18:44+08:00",
    updatedAtLabel: "2026-08-29 09:18:44",
  },
  relations: [],
  timeline: [
    {
      visibility: "readable",
      id: "activity:act_01K3CIRUNRECORDED",
      action: "CI Run 已记录为成功",
      detail: "Identity Service 的完成态构建事实已经进入统一时间线。",
      actorLabel: "受控自动化",
      sourceLabel: "ci-run.recorded",
      occurredAt: "2026-08-29T09:18:44+08:00",
      occurredAtLabel: "08-29 · 09:18",
    },
  ],
} satisfies NexusViewData;

export const failedCIRunNexusViewFixture = {
  ...succeededCIRunNexusViewFixture,
  current: {
    ...succeededCIRunNexusViewFixture.current,
    status: "failed",
    statusLabel: "构建失败",
    summary:
      "一次构建流水线已经完成，并被记录为失败。失败事实可被追踪，但不会自动创建 Deployment 或改变 Environment。",
    completedAt: "2026-08-29T10:07:31+08:00",
    completedAtLabel: "2026-08-29 10:07:31",
    recordedAt: "2026-08-29T10:07:33+08:00",
    recordedAtLabel: "2026-08-29 10:07:33",
    updatedAt: "2026-08-29T10:07:33+08:00",
    updatedAtLabel: "2026-08-29 10:07:33",
  },
  timeline: [
    {
      visibility: "readable",
      id: "activity:act_01K3CIRUNFAILED",
      action: "CI Run 已记录为失败",
      detail: "Identity Service 的失败构建事实已经进入统一时间线。",
      actorLabel: "受控自动化",
      sourceLabel: "ci-run.recorded",
      occurredAt: "2026-08-29T10:07:33+08:00",
      occurredAtLabel: "08-29 · 10:07",
    },
  ],
} satisfies NexusViewData;

export const succeededDeploymentNexusViewFixture = {
  current: {
    entityRef: "entity://deployment/dpl_01K3RADISHNEXUS",
    entityType: "deployment",
    eyebrow: "Deployment · Explicit staging fact",
    status: "succeeded",
    statusLabel: "部署成功",
    summary:
      "一名持有目标 Environment 显式授权的成员，记录了一次已经完成的 staging 部署事实。该记录保留构建来源，但不表示 RadishNexus 执行了外部部署。",
    environment: {
      entityRef: "entity://environment/env_staging",
      name: "Staging",
    },
    ciRun: {
      entityRef: "entity://ci-run/cir_01K3RADISHNEXUS",
      name: "来源 CI Run",
    },
    startedAt: "2026-08-29T09:20:03+08:00",
    startedAtLabel: "2026-08-29 09:20:03",
    completedAt: "2026-08-29T09:24:27+08:00",
    completedAtLabel: "2026-08-29 09:24:27",
    recordedAt: "2026-08-29T09:24:31+08:00",
    recordedAtLabel: "2026-08-29 09:24:31",
  },
  relations: [
    {
      visibility: "readable",
      entityRef: "entity://ci-run/cir_01K3RADISHNEXUS",
      entityType: "ci-run",
      entityTypeLabel: "CI Run",
      relationType: "deploys",
      relationLabel: "来源构建",
      title: "CI Run",
      summary:
        "这个完成态构建是本次 staging Deployment 的明确来源；构建成功本身不会自动产生 Deployment。",
    },
  ],
  timeline: [
    {
      visibility: "readable",
      id: "activity:act_01K3DEPLOYRECORDED",
      action: "staging Deployment 已记录为成功",
      detail: "Staging 与来源 CI Run 已作为权限过滤后的上下文进入统一时间线。",
      actorLabel: "萝卜",
      sourceLabel: "deployment.recorded",
      occurredAt: "2026-08-29T09:24:27+08:00",
      occurredAtLabel: "08-29 · 09:24",
    },
  ],
} satisfies NexusViewData;

export const failedDeploymentNexusViewFixture = {
  ...succeededDeploymentNexusViewFixture,
  current: {
    ...succeededDeploymentNexusViewFixture.current,
    status: "failed",
    statusLabel: "部署失败",
    summary:
      "来源构建已经成功，但一次独立的 staging 部署结果被明确记录为失败。失败不会改写 CI Run，也不会被界面表现为已部署成功。",
    completedAt: "2026-08-29T10:14:27+08:00",
    completedAtLabel: "2026-08-29 10:14:27",
    recordedAt: "2026-08-29T10:14:31+08:00",
    recordedAtLabel: "2026-08-29 10:14:31",
  },
  timeline: [
    {
      visibility: "readable",
      id: "activity:act_01K3DEPLOYFAILED",
      action: "staging Deployment 已记录为失败",
      detail:
        "外部部署失败事实已进入时间线；来源 CI Run 仍保持自己的成功构建事实。",
      actorLabel: "萝卜",
      sourceLabel: "deployment.recorded",
      occurredAt: "2026-08-29T10:14:27+08:00",
      occurredAtLabel: "08-29 · 10:14",
    },
  ],
} satisfies NexusViewData;
