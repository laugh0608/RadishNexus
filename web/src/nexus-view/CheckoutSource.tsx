import { useState } from "react";
import type { CICheckout, CheckoutReason } from "./model";
import { repositoryPagePath } from "../workspace/repository-ci-runs-api";

const reasons: Record<CheckoutReason, string> = {
  checkout_not_observed: "没有取得本次 checkout 的有效观察。",
  unsupported_checkout: "本次 checkout 超出当前支持的来源范围。",
  binding_changed_during_run: "构建期间来源配置发生变化。",
};

export function CheckoutSource({
  checkout,
  workspaceID,
}: {
  checkout: CICheckout;
  workspaceID?: string;
}) {
  const [message, setMessage] = useState("");
  if (checkout.visibility === "restricted")
    return (
      <section className="checkout-source" aria-label="来源上下文">
        <h2>来源上下文</h2>
        <p>此上下文当前不可读取。</p>
      </section>
    );
  if (checkout.state === "not_collected")
    return (
      <section className="checkout-source" aria-label="代码来源">
        <h2>代码来源</h2>
        <p>此构建未采集代码来源。</p>
      </section>
    );
  const path = workspaceID
    ? repositoryPagePath(workspaceID, checkout.repository.id)
    : null;
  return (
    <section className="checkout-source" aria-label="代码来源">
      <h2>代码来源</h2>
      <p>
        {path ? (
          <a href={path}>{checkout.repository.name}</a>
        ) : (
          checkout.repository.name
        )}
      </p>
      {checkout.state === "unavailable" ? (
        <>
          <p>构建结果已记录，checkout 来源不可用。</p>
          <p>{reasons[checkout.reason]}</p>
        </>
      ) : (
        <>
          <p>Checkout revision · SHA-1</p>
          <code className="checkout-oid">{checkout.revision.oid}</code>
          <button
            type="button"
            onClick={async () => {
              try {
                await navigator.clipboard.writeText(checkout.revision.oid);
                setMessage("已复制完整 commit。");
              } catch {
                setMessage("复制失败，请选择上方完整 commit 手动复制。");
              }
            }}
          >
            复制完整 commit
          </button>
          <p role="status">{message}</p>
          <p>
            仓库由管理员绑定，checkout revision 由 controller
            观察；不代表完整构建输入已验证或工单已交付。
          </p>
        </>
      )}
    </section>
  );
}
