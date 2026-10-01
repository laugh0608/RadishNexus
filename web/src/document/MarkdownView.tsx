import { Fragment } from "react";
import type { Node, View } from "./api";

export function MarkdownView({ view }: { view: View }) {
  return (
    <div className="markdown-view">
      {view.nodes.map((node, i) => (
        <MarkdownNode node={node} key={i} />
      ))}
    </div>
  );
}
function MarkdownNode({ node }: { node: Node }) {
  const children = node.children?.map((node, i) => (
    <MarkdownNode node={node} key={i} />
  ));
  switch (node.kind) {
    case "text":
      return <>{node.text}</>;
    case "paragraph":
      return <p>{children}</p>;
    case "group":
      return <Fragment>{children}</Fragment>;
    case "heading":
      switch (node.level) {
        case 1:
          return <h1>{children}</h1>;
        case 2:
          return <h2>{children}</h2>;
        case 3:
          return <h3>{children}</h3>;
        case 4:
          return <h4>{children}</h4>;
        case 5:
          return <h5>{children}</h5>;
        case 6:
          return <h6>{children}</h6>;
        default:
          throw new Error("未知标题等级");
      }
    case "quote":
      return <blockquote>{children}</blockquote>;
    case "list":
      return node.ordered ? (
        <ol start={node.start}>{children}</ol>
      ) : (
        <ul>{children}</ul>
      );
    case "item":
      return <li>{children}</li>;
    case "emphasis":
      return <em>{children}</em>;
    case "strong":
      return <strong>{children}</strong>;
    case "code":
      return <code>{node.text}</code>;
    case "code_block":
      return (
        <pre>
          <code>{node.text}</code>
        </pre>
      );
    case "rule":
      return <hr />;
    case "break":
      return <br />;
    case "link":
      return (
        <a
          href={node.url}
          target="_blank"
          rel="noopener noreferrer"
          referrerPolicy="no-referrer"
        >
          {children}
        </a>
      );
    default:
      throw new Error("未知 Markdown 节点");
  }
}
