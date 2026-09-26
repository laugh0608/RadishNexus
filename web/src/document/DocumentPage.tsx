import { useCallback, useEffect, useRef, useState } from "react";
import {
  documentClient,
  documentPath,
  documentsPath,
  DocumentError,
  format,
  type DocumentClient,
  type DocumentView,
  type HistoryItem,
  type Page,
  type Revision,
  type View,
  type WriteInput,
  type ListItem,
} from "./api";
import { MarkdownView } from "./MarkdownView";

function message(e: unknown) {
  return e instanceof Error ? e.message : "文档服务暂不可用。";
}
function revoked(e: unknown) {
  return e instanceof DocumentError && [401, 403, 404].includes(e.status ?? 0);
}
function useDraftGuard(dirty: boolean) {
  useEffect(() => {
    if (!dirty) return;
    const unload = (event: BeforeUnloadEvent) => {
      event.preventDefault();
      event.returnValue = "";
    };
    const discard = (event: Event) => {
      if (!window.confirm("有未保存的草稿，离开后将丢失。确定离开？"))
        event.preventDefault();
    };
    const link = (event: MouseEvent) => {
      if (
        event.target instanceof Element &&
        event.target.closest("a[href]") &&
        !event.target.closest('a[target="_blank"]') &&
        !event.target.closest('a[href^="#"]')
      )
        discard(event);
    };
    window.addEventListener("beforeunload", unload);
    document.addEventListener("click", link, true);
    window.addEventListener("radishnexus-before-discard", discard);
    return () => {
      window.removeEventListener("beforeunload", unload);
      document.removeEventListener("click", link, true);
      window.removeEventListener("radishnexus-before-discard", discard);
    };
  }, [dirty]);
}
function Source({ revision }: { revision: Revision }) {
  const [source, setSource] = useState(false);
  if (revision.view) return <MarkdownView view={revision.view} />;
  return (
    <div role="alert">
      <p>
        这个版本暂时无法安全展示：{revision.rendering_failure?.code}。原始
        Markdown 仍然保留。
      </p>
      <button type="button" onClick={() => setSource(!source)}>
        {source ? "收起源码" : "查看原始 Markdown"}
      </button>
      {source ? <pre>{revision.body_markdown}</pre> : null}
    </div>
  );
}
function DocumentSources({
  workspaceID,
  relations,
}: {
  workspaceID: string;
  relations: DocumentView["relations"];
}) {
  if (relations.length === 0)
    return <p className="document-help">暂无可见来源。</p>;
  return (
    <ul>
      {relations.map((r, i) => (
        <li key={i}>
          {r.visibility === "restricted" ? (
            "受限来源"
          ) : (
            <a
              href={`/workspaces/${encodeURIComponent(workspaceID)}/tickets/${encodeURIComponent(r.target.ref.id)}`}
            >
              {r.target.title}
            </a>
          )}
        </li>
      ))}
    </ul>
  );
}

export function DocumentPage({
  workspaceID,
  documentID,
  client = documentClient,
  onSessionExpired,
}: {
  workspaceID: string;
  documentID: string;
  client?: DocumentClient;
  onSessionExpired: () => void;
}) {
  const [view, setView] = useState<DocumentView | null>(null),
    [title, setTitle] = useState(""),
    [body, setBody] = useState(""),
    [base, setBase] = useState(0),
    [editing, setEditing] = useState(false);
  const [error, setError] = useState(""),
    [notice, setNotice] = useState(""),
    [busy, setBusy] = useState(false),
    [denied, setDenied] = useState(false),
    [conflict, setConflict] = useState(false);
  const [preview, setPreview] = useState<View | null>(null),
    [history, setHistory] = useState<Page<HistoryItem> | null>(null),
    [selected, setSelected] = useState<Revision | null>(null);
  const [informationOpen, setInformationOpen] = useState(false);
  const informationButton = useRef<HTMLButtonElement>(null);
  const informationPanel = useRef<HTMLElement>(null);
  const readingScroll = useRef(0);
  const historyHeading = useRef<HTMLHeadingElement>(null);
  const titleInput = useRef<HTMLInputElement>(null);
  useEffect(() => {
    if (informationOpen) informationPanel.current?.focus();
  }, [informationOpen]);
  useEffect(() => {
    if (history) historyHeading.current?.focus();
  }, [history]);
  useEffect(() => {
    if (editing) titleInput.current?.focus();
  }, [editing]);
  const closeInformation = () => {
    setInformationOpen(false);
    requestAnimationFrame(() => {
      informationButton.current?.focus({ preventScroll: true });
      window.scrollTo(0, readingScroll.current);
    });
  };
  const [pending, setPending] = useState<{
    kind: "save" | "restore";
    input: WriteInput;
  } | null>(null);
  const mounted = useRef(true),
    generation = useRef(0),
    draftChanged = useRef(false),
    busyRef = useRef(false),
    previewRequest = useRef<AbortController | null>(null);
  const dirty =
    editing &&
    (title !== (view?.current.title ?? "") ||
      body !== (view?.current.body_markdown ?? ""));
  useEffect(() => {
    draftChanged.current = dirty;
  }, [dirty]);
  useDraftGuard(dirty || pending !== null);
  const fail = useCallback(
    (e: unknown) => {
      if (!mounted.current) return;
      setError(message(e));
      if (revoked(e)) {
        generation.current++;
        previewRequest.current?.abort();
        setInformationOpen(false);
        setNotice("");
        setView(null);
        setTitle("");
        setBody("");
        setBase(0);
        setEditing(false);
        setPreview(null);
        setHistory(null);
        setSelected(null);
        setPending(null);
        setConflict(false);
        setDenied(true);
        if (e instanceof DocumentError && e.status === 401) onSessionExpired();
      }
    },
    [onSessionExpired],
  );
  const baseRef = useRef(base);
  useEffect(() => {
    baseRef.current = base;
  }, [base]);
  const load = useCallback(() => {
    const ticket = ++generation.current;
    return Promise.resolve()
      .then(() => client.read(workspaceID, documentID))
      .then(
        (next) => {
          if (!mounted.current || ticket !== generation.current) return;
          setView(next);
          setDenied(false);
          if (!draftChanged.current) {
            setTitle(next.current.title);
            setBody(next.current.body_markdown);
            setBase(next.current.revision);
          } else if (next.current.revision !== baseRef.current)
            setConflict(true);
        },
        (e) => {
          if (ticket === generation.current) fail(e);
        },
      );
  }, [client, workspaceID, documentID, fail]);
  useEffect(() => {
    mounted.current = true;
    void load();
    const focus = () => {
      if (!busyRef.current) void load();
    };
    window.addEventListener("focus", focus);
    const visible = () => {
      if (document.visibilityState === "visible") focus();
    };
    document.addEventListener("visibilitychange", visible);
    return () => {
      mounted.current = false;
      window.removeEventListener("focus", focus);
      document.removeEventListener("visibilitychange", visible);
    };
  }, [load]);
  const act = async (work: () => Promise<void>) => {
    if (busyRef.current) return;
    busyRef.current = true;
    setBusy(true);
    setError("");
    try {
      await work();
    } catch (e) {
      fail(e);
    } finally {
      busyRef.current = false;
      if (mounted.current) setBusy(false);
    }
  };
  const write = async (kind: "save" | "restore") =>
    act(async () => {
      if (!view) return;
      const operation = pending ?? {
        kind,
        input:
          kind === "save"
            ? {
                client_operation_id: crypto.randomUUID(),
                base_revision: base,
                title,
                body_markdown: body,
                format_version: format,
              }
            : {
                client_operation_id: crypto.randomUUID(),
                base_revision: view.current.revision,
                restore_revision: selected!.revision,
                confirmed: true as const,
              },
      };
      setPending(operation);
      generation.current++;
      try {
        const result = await client[operation.kind](
          workspaceID,
          documentID,
          operation.input,
        );
        if (!mounted.current) return;
        setPending(null);
        setEditing(false);
        setConflict(false);
        setSelected(null);
        setHistory(null);
        setPreview(null);
        draftChanged.current = false;
        setNotice(`已保存版本 ${result.applied_revision}，正在读取当前版本。`);
        await load();
      } catch (e) {
        if (
          e instanceof DocumentError &&
          e.status !== undefined &&
          e.status < 500
        ) {
          setPending(null);
          if (e.status === 409) {
            setConflict(true);
            await load();
          }
        }
        throw e;
      }
    });
  const loadHistory = (cursor?: string) =>
    act(async () => {
      const ticket = generation.current;
      const result = await client.history(workspaceID, documentID, cursor);
      if (mounted.current && ticket === generation.current) setHistory(result);
    });
  const readHistory = (n: number) =>
    act(async () => {
      const ticket = generation.current;
      const result = await client.revision(workspaceID, documentID, n);
      if (mounted.current && ticket === generation.current) setSelected(result);
    });
  const getPreview = () =>
    act(async () => {
      if (!view) return;
      previewRequest.current?.abort();
      const controller = new AbortController();
      previewRequest.current = controller;
      const result = await client.preview(
        workspaceID,
        view.current.project.id,
        body,
        controller.signal,
      );
      if (mounted.current && !controller.signal.aborted) setPreview(result);
    });
  const change = () => {
    previewRequest.current?.abort();
    setPreview(null);
    setNotice("");
  };
  const startEditing = () => {
    setInformationOpen(false);
    setEditing(true);
    setBase(view!.current.revision);
    setTitle(view!.current.title);
    setBody(view!.current.body_markdown);
  };
  const discardDraft = () => {
    if (!dirty || window.confirm("确定放弃未保存的草稿？")) {
      setEditing(false);
      setConflict(false);
      setTitle(view!.current.title);
      setBody(view!.current.body_markdown);
      setBase(view!.current.revision);
      setPreview(null);
    }
  };
  return (
    <main
      className={`document-workspace${informationOpen ? " document-workspace--information" : ""}`}
    >
      <header className="document-toolbar">
        <nav aria-label="文档位置">
          <a href="/">工作区</a>
          {view ? (
            <>
              <span aria-hidden="true">/</span>
              <a href={documentsPath(workspaceID, view.current.project.id)}>
                项目文档
              </a>
            </>
          ) : null}
        </nav>
        {view ? (
          <div className="document-actions">
            {!editing ? (
              <button
                ref={informationButton}
                type="button"
                aria-expanded={informationOpen}
                aria-controls="document-information"
                onClick={() => {
                  if (informationOpen) closeInformation();
                  else {
                    readingScroll.current = window.scrollY;
                    setInformationOpen(true);
                  }
                }}
              >
                {informationOpen ? "收起信息" : "文档信息"}
              </button>
            ) : null}
            <button
              type="button"
              disabled={busy || pending !== null}
              onClick={() => {
                if (informationOpen) setInformationOpen(false);
                void loadHistory();
              }}
            >
              版本历史
            </button>
            {editing ? (
              <>
                <button
                  type="button"
                  disabled={busy || pending !== null}
                  onClick={discardDraft}
                >
                  放弃草稿
                </button>
                <button
                  className="primary-button"
                  type="button"
                  disabled={
                    busy ||
                    conflict ||
                    !dirty ||
                    !title.trim() ||
                    pending !== null
                  }
                  onClick={() => void write("save")}
                >
                  保存新版本
                </button>
              </>
            ) : (
              <button
                className="primary-button"
                type="button"
                disabled={busy || pending !== null}
                onClick={startEditing}
              >
                编辑文档
              </button>
            )}
          </div>
        ) : null}
      </header>
      {error ? (
        <p className="document-feedback document-feedback--error" role="alert">
          {error}
        </p>
      ) : null}
      {notice ? (
        <p className="document-feedback" role="status">
          {notice}
        </p>
      ) : null}
      {!view ? (
        <section className="document-empty">
          <p className="section-kicker">Document</p>
          <h1>
            {denied
              ? "文档不可用"
              : error
                ? "文档暂时无法读取"
                : "正在读取文档"}
          </h1>
          <p>
            {denied
              ? "请返回工作区，查看当前可访问的内容。"
              : "读取成功后会显示文档及其当前版本。"}
          </p>
          <button type="button" disabled={busy} onClick={() => void load()}>
            重新读取
          </button>
        </section>
      ) : (
        <div className="document-content">
          <div
            className={`document-primary${editing ? " document-primary--editing" : ""}`}
          >
            {editing ? (
              <section className="document-editor" aria-label="编辑文档">
                <div className="document-metadata">
                  <span className="document-badge document-badge--warning">
                    {dirty ? "未保存更改" : "编辑中"}
                  </span>
                  <span>基于版本 {base}</span>
                </div>
                <h1 className="visually-hidden">编辑文档</h1>
                <label className="document-title-input">
                  文档标题
                  <input
                    ref={titleInput}
                    disabled={busy || pending !== null}
                    value={title}
                    onChange={(e) => {
                      change();
                      setTitle(e.target.value);
                    }}
                  />
                </label>
                <div className="document-editor-tools">
                  <label htmlFor="document-markdown">Markdown 正文</label>
                  <button
                    type="button"
                    disabled={busy || pending !== null}
                    onClick={() => void getPreview()}
                  >
                    预览草稿
                  </button>
                </div>
                <textarea
                  id="document-markdown"
                  rows={16}
                  disabled={busy || pending !== null}
                  value={body}
                  onChange={(e) => {
                    change();
                    setBody(e.target.value);
                  }}
                  aria-describedby="draft-help"
                />
                <p className="document-help" id="draft-help">
                  草稿仅保留在当前页面，离开前请保存或放弃。支持基础
                  Markdown，图片、HTML、表格等扩展不受支持。
                </p>
                {preview ? (
                  <section className="document-panel" aria-label="草稿预览">
                    <h2>草稿预览</h2>
                    <MarkdownView view={preview} />
                  </section>
                ) : null}
                {conflict ? (
                  <section
                    className="document-panel document-conflict"
                    role="alert"
                  >
                    <h2>请比较最新版本与本地草稿</h2>
                    <p>
                      草稿仍保留在上方。当前版本 {view.current.revision}
                      ，草稿基于版本 {base}。请手动调整后确认重新应用。
                    </p>
                    <h3>{view.current.title}</h3>
                    <pre>{view.current.body_markdown}</pre>
                    <button
                      className="primary-button"
                      type="button"
                      disabled={busy || pending !== null}
                      onClick={() => {
                        setBase(view.current.revision);
                        setConflict(false);
                        setNotice("已选择以最新版本为基础，请确认草稿后保存。");
                      }}
                    >
                      基于最新版本重新应用草稿
                    </button>
                  </section>
                ) : null}
              </section>
            ) : (
              <article className="document-article">
                <header className="document-heading">
                  <h1>{view.current.title}</h1>
                  <div className="document-metadata">
                    <span className="document-badge">
                      版本 {view.current.revision}
                    </span>
                    <span>
                      {view.current.created_by.id} 更新于{" "}
                      <time dateTime={view.current.created_at}>
                        {new Date(view.current.created_at).toLocaleString()}
                      </time>
                    </span>
                  </div>
                  {view.relations.length ? (
                    <div
                      className="document-source-links"
                      aria-label="文档来源"
                    >
                      {" "}
                      <DocumentSources
                        workspaceID={workspaceID}
                        relations={view.relations}
                      />
                    </div>
                  ) : null}
                </header>
                <Source key={view.current.revision} revision={view.current} />
              </article>
            )}
            {pending ? (
              <section
                className="document-panel document-conflict"
                role="alert"
              >
                <h2>提交结果尚未确认</h2>
                <p>
                  已保留原输入与操作标识，请重试原操作。确认结果前不能继续修改。
                </p>
                <button
                  type="button"
                  disabled={busy}
                  onClick={() => void write(pending.kind)}
                >
                  重试原操作
                </button>
              </section>
            ) : null}
            {history ? (
              <section className="document-panel" aria-label="版本历史">
                <h2 ref={historyHeading} tabIndex={-1}>
                  版本历史
                </h2>
                <ol>
                  {history.items.map((item) => (
                    <li key={item.revision}>
                      <button
                        type="button"
                        disabled={busy || pending !== null}
                        onClick={() => void readHistory(item.revision)}
                      >
                        版本 {item.revision} · {item.title}
                      </button>
                      <span>
                        {" "}
                        {item.created_by.id} ·{" "}
                        {new Date(item.created_at).toLocaleString()}
                        {item.restored_from_revision
                          ? ` · 恢复自版本 ${item.restored_from_revision}`
                          : ""}
                      </span>
                    </li>
                  ))}
                </ol>
                <button
                  type="button"
                  disabled={busy || pending !== null || !history.next_cursor}
                  onClick={() =>
                    void loadHistory(history.next_cursor ?? undefined)
                  }
                >
                  更早版本
                </button>
              </section>
            ) : null}
            {selected ? (
              <section className="document-panel" aria-label="历史版本">
                <h2>
                  历史版本 {selected.revision} · {selected.title}
                </h2>
                <Source key={selected.revision} revision={selected} />
                <h3>
                  当前版本 {view.current.revision} · {view.current.title}
                </h3>
                <Source
                  key={`current-${view.current.revision}`}
                  revision={view.current}
                />
                <p>
                  恢复会将所选历史内容追加为新版本，保留现有历史。未保存草稿需先保存或放弃。
                </p>
                <button
                  type="button"
                  disabled={
                    busy || dirty || pending !== null || selected.view === null
                  }
                  onClick={() => {
                    if (
                      window.confirm(
                        `将版本 ${selected.revision} 的内容恢复为新版本？`,
                      )
                    )
                      void write("restore");
                  }}
                >
                  确认恢复为新版本
                </button>
              </section>
            ) : null}

            <footer className="document-reading-footer">
              <span>当前已保存版本 {view.current.revision}</span>
              <button
                type="button"
                disabled={busy || pending !== null}
                onClick={() => void load()}
              >
                读取最新版本
              </button>
            </footer>
          </div>
          {informationOpen ? (
            <aside
              id="document-information"
              className="document-information"
              ref={informationPanel}
              tabIndex={-1}
              aria-label="文档信息"
              onKeyDown={(event) => {
                if (event.key === "Escape") {
                  event.preventDefault();
                  closeInformation();
                }
              }}
            >
              <div className="document-information-heading">
                <h2>文档信息</h2>
                <button type="button" onClick={closeInformation}>
                  返回阅读
                </button>
              </div>
              <h3 className="document-information-title">
                {view.current.title}
              </h3>
              <section>
                <h3>可见范围</h3>
                <p>Project 可读成员</p>
                <p className="document-help">引用不会授予额外权限。</p>
              </section>
              <section>
                <h3>来源 Ticket</h3>{" "}
                <DocumentSources
                  workspaceID={workspaceID}
                  relations={view.relations}
                />
              </section>
              <section>
                <h3>最近活动</h3>
                {view.timeline.length === 0 ? (
                  <p className="document-help">暂无可见活动。</p>
                ) : null}
                <ol>
                  {view.timeline.map((item) => (
                    <li key={item.id}>
                      版本 {item.revision} ·{" "}
                      {item.restored_from_revision
                        ? `恢复自 ${item.restored_from_revision}`
                        : item.activity_type === "document.created"
                          ? "创建"
                          : "保存"}{" "}
                      · {item.actor.id} ·{" "}
                      {new Date(item.occurred_at).toLocaleString()}
                    </li>
                  ))}
                </ol>
              </section>
            </aside>
          ) : null}
        </div>
      )}
    </main>
  );
}

export function DocumentList({
  workspaceID,
  projectID,
  client = documentClient,
  onSessionExpired,
}: {
  workspaceID: string;
  projectID: string;
  client?: DocumentClient;
  onSessionExpired: () => void;
}) {
  const [page, setPage] = useState<Page<ListItem> | null>(null),
    [error, setError] = useState(""),
    [cursor, setCursor] = useState<string | undefined>(),
    [reload, setReload] = useState(0);
  useEffect(() => {
    const c = new AbortController();
    void client.list(workspaceID, projectID, cursor, c.signal).then(
      (p) => {
        if (!c.signal.aborted) setPage(p);
      },
      (e) => {
        if (c.signal.aborted) return;
        setPage(null);
        setError(message(e));
        if (e instanceof DocumentError && e.status === 401) onSessionExpired();
      },
    );
    return () => c.abort();
  }, [client, workspaceID, projectID, cursor, reload, onSessionExpired]);
  useEffect(() => {
    const refresh = () => {
      setPage(null);
      setCursor(undefined);
      setReload((n) => n + 1);
    };
    window.addEventListener("focus", refresh);
    return () => window.removeEventListener("focus", refresh);
  }, []);
  return (
    <main className="document-layout document-list">
      <nav>
        <a href="/">工作区</a>
      </nav>
      <h1>项目文档</h1>
      <p>从 Ticket 创建设计文档，在这里查找与继续编辑。</p>
      {error ? <p role="alert">{error}</p> : null}
      <button
        type="button"
        onClick={() => {
          setPage(null);
          setError("");
          setCursor(undefined);
          setReload((n) => n + 1);
        }}
      >
        刷新文档列表
      </button>
      {page ? (
        <>
          <ul>
            {page.items.map((item) => (
              <li key={item.ref.id}>
                <a href={documentPath(workspaceID, item.ref.id)}>
                  {item.title}
                </a>{" "}
                · 版本 {item.revision} ·{" "}
                {new Date(item.updated_at).toLocaleString()}
              </li>
            ))}
          </ul>
          {page.items.length === 0 ? <p>当前没有可访问的文档。</p> : null}
          <button
            type="button"
            disabled={!page.next_cursor}
            onClick={() => {
              setCursor(page.next_cursor ?? undefined);
              setPage(null);
            }}
          >
            下一页文档
          </button>
        </>
      ) : error ? null : (
        <p role="status">正在加载文档…</p>
      )}
    </main>
  );
}

export function CreateDocument({
  workspaceID,
  ticketID,
  projectID,
  onSessionExpired,
  onRevoked,
  client = documentClient,
}: {
  workspaceID: string;
  ticketID: string;
  projectID: string;
  onSessionExpired: () => void;
  onRevoked?: () => void;
  client?: DocumentClient;
}) {
  const [title, setTitle] = useState(""),
    [body, setBody] = useState(""),
    [preview, setPreview] = useState<View | null>(null),
    [error, setError] = useState(""),
    [busy, setBusy] = useState(false),
    [pending, setPending] = useState<WriteInput | null>(null),
    [created, setCreated] = useState<string | null>(null),
    [denied, setDenied] = useState(false);
  const mounted = useRef(true),
    busyRef = useRef(false);
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);
  useDraftGuard(title !== "" || body !== "" || pending !== null);
  const run = async (create: boolean) => {
    if (busyRef.current) return;
    busyRef.current = true;
    setBusy(true);
    setError("");
    try {
      if (create) {
        const input = pending ?? {
          client_operation_id: crypto.randomUUID(),
          title,
          body_markdown: body,
          format_version: format,
        };
        setPending(input);
        const result = await client.create(workspaceID, ticketID, input);
        if (!mounted.current) return;
        setCreated(result.ref.id);
        setTitle("");
        setBody("");
        setPreview(null);
        setPending(null);
      } else {
        const view = await client.preview(workspaceID, projectID, body);
        if (mounted.current) setPreview(view);
      }
    } catch (e) {
      if (!mounted.current) return;
      setError(message(e));
      if (
        e instanceof DocumentError &&
        e.status !== undefined &&
        e.status < 500
      )
        setPending(null);
      if (revoked(e)) {
        setTitle("");
        setBody("");
        setPreview(null);
        setCreated(null);
        setDenied(true);
        onRevoked?.();
        if (e instanceof DocumentError && e.status === 401) onSessionExpired();
      }
    } finally {
      busyRef.current = false;
      if (mounted.current) setBusy(false);
    }
  };
  return (
    <section aria-label="创建设计文档">
      <h3>创建设计文档</h3>
      <p>
        文档及全部历史对当前 Project 的可读成员开放。请确认填写内容适合此范围。
      </p>
      <p>草稿仅在当前页面保留，刷新或退出后丢失。</p>
      {!denied ? (
        <>
          <label>
            文档标题
            <input
              value={title}
              disabled={busy || pending !== null}
              onChange={(e) => setTitle(e.target.value)}
            />
          </label>
          <label>
            Markdown 正文
            <textarea
              rows={8}
              value={body}
              disabled={busy || pending !== null}
              onChange={(e) => {
                setBody(e.target.value);
                setPreview(null);
              }}
            />
          </label>
          <button
            type="button"
            disabled={busy || pending !== null}
            onClick={() => void run(false)}
          >
            预览文档
          </button>
          <button
            type="button"
            disabled={busy || !title.trim()}
            onClick={() => void run(true)}
          >
            {pending ? "重试原创建操作" : "创建设计文档"}
          </button>
        </>
      ) : null}
      {error ? <p role="alert">{error}</p> : null}
      {preview ? <MarkdownView view={preview} /> : null}
      {created ? (
        <p role="status">
          文档已创建。
          <a href={documentPath(workspaceID, created)}>打开设计文档</a>
        </p>
      ) : null}
    </section>
  );
}
