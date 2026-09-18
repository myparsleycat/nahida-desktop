import ReactMarkdown from "react-markdown";
import remarkBreaks from "remark-breaks";
import remarkGfm from "remark-gfm";

/**
 * Render assistant-authored markdown. `remark-breaks` keeps single newlines as visible line
 * breaks, matching how the model writes multi-line replies; block spacing comes from
 * `markdown.css` because Tailwind's preflight removes every block margin.
 */
export function Markdown({ text, streaming = false }: { text: string; streaming?: boolean }) {
  return (
    <div className="markdown-body text-sm leading-relaxed">
      <ReactMarkdown remarkPlugins={[remarkGfm, remarkBreaks]}>{text}</ReactMarkdown>
      {streaming && (
        <span className="markdown-caret ml-1 inline-block h-4 w-1 animate-pulse bg-accent" />
      )}
    </div>
  );
}
