import Markdown from "react-markdown";
import remarkGfm from "remark-gfm";

function linkURL(value: string) {
  if (value.startsWith("#")) return value;
  try {
    const url = new URL(value);
    if (["https:", "http:"].includes(url.protocol) && !url.username && !url.password) return url.href;
  } catch { /* Unsafe and relative destinations remain plain text. */ }
  return "";
}

export function ChatReply({ text }: { text: string }) {
  return <div className="chat-reply"><Markdown skipHtml remarkPlugins={[remarkGfm]} urlTransform={linkURL} components={{
    a: ({ href, children }) => href ? <a href={href} target={href.startsWith("#") ? undefined : "_blank"} rel="noopener noreferrer">{children}</a> : <span>{children}</span>,
    img: ({ alt }) => <span className="chat-image-note">{alt || "图片"}</span>,
    table: ({ children }) => <div className="chat-table"><table>{children}</table></div>,
  }}>{text}</Markdown></div>;
}
