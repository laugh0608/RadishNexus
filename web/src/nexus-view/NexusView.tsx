import type {
  NexusRelation,
  NexusTimelineItem,
  NexusViewData,
  NexusViewState,
} from "./model";

interface NexusViewProps {
  state: NexusViewState;
  onRetry?: () => void;
}

export function NexusView({ state, onRetry }: NexusViewProps) {
  if (state.status === "loading") {
    return <LoadingNexusView />;
  }

  if (state.status === "error") {
    return <ErrorNexusView message={state.message} onRetry={onRetry} />;
  }

  return <ReadyNexusView data={state.data} />;
}

function LoadingNexusView() {
  return (
    <main className="nexus-layout" aria-busy="true">
      <p className="sr-only" role="status">
        正在加载 Nexus View
      </p>
      <section
        className="current-card current-card--loading"
        aria-hidden="true"
      >
        <div className="skeleton skeleton--eyebrow" />
        <div className="skeleton skeleton--title" />
        <div className="skeleton skeleton--title skeleton--short" />
        <div className="skeleton skeleton--copy" />
        <div className="skeleton skeleton--copy skeleton--short" />
      </section>
      <div className="nexus-columns" aria-hidden="true">
        <section className="panel panel--loading">
          <div className="skeleton skeleton--heading" />
          <div className="skeleton-card" />
          <div className="skeleton-card" />
        </section>
        <section className="panel panel--loading">
          <div className="skeleton skeleton--heading" />
          <div className="skeleton-row" />
          <div className="skeleton-row" />
          <div className="skeleton-row" />
        </section>
      </div>
    </main>
  );
}

interface ErrorNexusViewProps {
  message: string;
  onRetry?: () => void;
}

function ErrorNexusView({ message, onRetry }: ErrorNexusViewProps) {
  return (
    <main className="nexus-layout">
      <section className="state-panel state-panel--error" role="alert">
        <span className="state-panel__mark" aria-hidden="true">
          !
        </span>
        <p className="section-kicker">Nexus View 暂不可用</p>
        <h1>上下文没有成功载入</h1>
        <p>{message}</p>
        {onRetry ? (
          <button className="primary-button" type="button" onClick={onRetry}>
            重新载入
          </button>
        ) : null}
      </section>
    </main>
  );
}

function ReadyNexusView({ data }: { data: NexusViewData }) {
  const { current } = data;

  return (
    <main className="nexus-layout">
      <article className="current-card" aria-labelledby="nexus-current-title">
        <div className="current-card__topline">
          <p className="section-kicker">{current.eyebrow}</p>
          <span className={`status-badge status-badge--${current.status}`}>
            <span aria-hidden="true" />
            {current.statusLabel}
          </span>
        </div>

        <div className="current-card__body">
          <div>
            <h1 id="nexus-current-title">{current.title}</h1>
            <p className="current-card__summary">{current.summary}</p>
          </div>
          <div className="entity-stamp" aria-label="稳定实体引用">
            <span>EntityRef</span>
            <code>{current.entityRef}</code>
          </div>
        </div>

        <dl className="current-meta">
          <MetaItem label="Governing Project" value={current.projectLabel} />
          <MetaItem label="Decision Owner" value={current.decisionOwnerLabel} />
          <MetaItem
            label="Evidence"
            value={`${current.evidenceCount} 条可读依据`}
          />
          <div>
            <dt>Last activity</dt>
            <dd>
              <time dateTime={current.updatedAt}>{current.updatedAtLabel}</time>
            </dd>
          </div>
        </dl>
      </article>

      <div className="nexus-columns">
        <RelationsPanel relations={data.relations} />
        <TimelinePanel timeline={data.timeline} />
      </div>
    </main>
  );
}

function MetaItem({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <dt>{label}</dt>
      <dd>{value}</dd>
    </div>
  );
}

function RelationsPanel({
  relations,
}: {
  relations: readonly NexusRelation[];
}) {
  return (
    <section className="panel" aria-labelledby="relations-title">
      <PanelHeading
        eyebrow="Connected context"
        title="Relations"
        count={relations.length}
        id="relations-title"
      />

      {relations.length === 0 ? (
        <EmptyState
          title="暂无关联上下文"
          description="当前 Decision 还没有你可以看到的关联对象。"
        />
      ) : (
        <ul className="relation-list">
          {relations.map((relation, index) => (
            <RelationCard
              relation={relation}
              key={relationKey(relation, index)}
            />
          ))}
        </ul>
      )}
    </section>
  );
}

function RelationCard({ relation }: { relation: NexusRelation }) {
  if (relation.visibility === "restricted") {
    return (
      <li className="relation-card relation-card--restricted">
        <span className="restricted-mark" aria-hidden="true">
          ···
        </span>
        <div>
          <p className="relation-card__label">受限对象</p>
          <p className="relation-card__restricted-copy">
            此处存在一项你无权读取的上下文。目标身份与关系信息未发送到客户端。
          </p>
        </div>
      </li>
    );
  }

  return (
    <li className="relation-card">
      <div className="relation-card__rail" aria-hidden="true" />
      <div className="relation-card__content">
        <div className="relation-card__topline">
          <span className="relation-card__label">{relation.relationLabel}</span>
          <span className="entity-kind">{relation.entityTypeLabel}</span>
        </div>
        <h3>{relation.title}</h3>
        <p>{relation.summary}</p>
        <code>{relation.entityRef}</code>
      </div>
    </li>
  );
}

function TimelinePanel({
  timeline,
}: {
  timeline: readonly NexusTimelineItem[];
}) {
  return (
    <section className="panel panel--timeline" aria-labelledby="timeline-title">
      <PanelHeading
        eyebrow="Permission-filtered activity"
        title="Timeline"
        count={timeline.length}
        id="timeline-title"
      />

      {timeline.length === 0 ? (
        <EmptyState
          title="暂无可见动态"
          description="新的领域事实出现后，会按当前权限投影到这里。"
        />
      ) : (
        <ol className="timeline-list">
          {timeline.map((item, index) => (
            <TimelineRow item={item} key={timelineKey(item, index)} />
          ))}
        </ol>
      )}
    </section>
  );
}

function TimelineRow({ item }: { item: NexusTimelineItem }) {
  if (item.visibility === "restricted") {
    return (
      <li className="timeline-row timeline-row--restricted">
        <span className="timeline-dot" aria-hidden="true" />
        <div>
          <p className="timeline-row__action">受限动态</p>
          <p>一项不可读活动已被收敛为通用占位，不提供来源或目标线索。</p>
        </div>
      </li>
    );
  }

  return (
    <li className="timeline-row">
      <span className="timeline-dot" aria-hidden="true" />
      <time dateTime={item.occurredAt}>{item.occurredAtLabel}</time>
      <div>
        <p className="timeline-row__action">{item.action}</p>
        <p>{item.detail}</p>
        <p className="timeline-row__source">
          {item.actorLabel} · {item.sourceLabel}
        </p>
      </div>
    </li>
  );
}

interface PanelHeadingProps {
  eyebrow: string;
  title: string;
  count: number;
  id: string;
}

function PanelHeading({ eyebrow, title, count, id }: PanelHeadingProps) {
  return (
    <header className="panel-heading">
      <div>
        <p className="section-kicker">{eyebrow}</p>
        <h2 id={id}>{title}</h2>
      </div>
      <span className="panel-count" aria-label={`${count} 项`}>
        {String(count).padStart(2, "0")}
      </span>
    </header>
  );
}

function EmptyState({
  title,
  description,
}: {
  title: string;
  description: string;
}) {
  return (
    <div className="empty-state">
      <span aria-hidden="true">○</span>
      <h3>{title}</h3>
      <p>{description}</p>
    </div>
  );
}

function relationKey(relation: NexusRelation, index: number) {
  return relation.visibility === "readable"
    ? relation.entityRef
    : `restricted-${index}`;
}

function timelineKey(item: NexusTimelineItem, index: number) {
  return item.visibility === "readable" ? item.id : `restricted-${index}`;
}
