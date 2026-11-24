// Markdown 渲染：成果物 inline 内容按 Markdown 展示（console.md「材料是
// 引用不是内容」——inline 材料即 Markdown 文本）。项目无全局 CSS，样式
// 以 antd Typography token 为基准内联实现，保持与控制台视觉一致。
import { Typography } from 'antd'
import ReactMarkdown from 'react-markdown'
import type { CSSProperties } from 'react'

const HEADING_SIZES: Record<number, number> = { 1: 20, 2: 17, 3: 15, 4: 14, 5: 13, 6: 13 }

const styles: Record<string, CSSProperties> = {
  p: { margin: '0 0 8px', lineHeight: 1.75 },
  h1: { margin: '16px 0 8px', fontWeight: 600 },
  h2: { margin: '16px 0 8px', fontWeight: 600 },
  h3: { margin: '12px 0 6px', fontWeight: 600 },
  h4: { margin: '12px 0 6px', fontWeight: 600 },
  h5: { margin: '12px 0 6px', fontWeight: 600 },
  h6: { margin: '12px 0 6px', fontWeight: 600 },
  ul: { margin: '0 0 8px', paddingLeft: 22 },
  ol: { margin: '0 0 8px', paddingLeft: 26 },
  li: { margin: '2px 0' },
  blockquote: {
    margin: '8px 0',
    padding: '4px 12px',
    borderLeft: '3px solid #d46b08',
    background: '#fff7e6',
    borderRadius: 2,
    color: '#595959',
  },
  code: {
    background: 'rgba(0, 0, 0, 0.06)',
    padding: '2px 5px',
    borderRadius: 4,
    fontSize: '0.9em',
    fontFamily:
      "ui-monospace, SFMono-Regular, 'SF Mono', Menlo, Consolas, 'Liberation Mono', monospace",
  },
  pre: {
    margin: '8px 0',
    padding: 12,
    background: '#f6f8fa',
    border: '1px solid #eaeef2',
    borderRadius: 6,
    overflowX: 'auto',
  },
  table: { borderCollapse: 'collapse', margin: '8px 0', display: 'block', overflowX: 'auto' },
  th: { border: '1px solid #e8e8e8', padding: '6px 12px', background: '#fafafa', fontWeight: 600 },
  td: { border: '1px solid #e8e8e8', padding: '6px 12px' },
  tr: { borderTop: '1px solid #e8e8e8' },
  hr: { border: 0, borderTop: '1px solid #e8e8e8', margin: '12px 0' },
  a: { color: '#1677ff' },
  img: { maxWidth: '100%' },
}

/** Markdown 块级渲染（成果物详情、长文内容）。 */
export function Markdown({ text }: { text: string }) {
  return (
    <div className="chalkline-md">
      <ReactMarkdown
        components={{
          h1: ({ children }) => <h1 style={{ ...styles.h1, fontSize: HEADING_SIZES[1] }}>{children}</h1>,
          h2: ({ children }) => <h2 style={{ ...styles.h2, fontSize: HEADING_SIZES[2] }}>{children}</h2>,
          h3: ({ children }) => <h3 style={{ ...styles.h3, fontSize: HEADING_SIZES[3] }}>{children}</h3>,
          h4: ({ children }) => <h4 style={{ ...styles.h4, fontSize: HEADING_SIZES[4] }}>{children}</h4>,
          h5: ({ children }) => <h5 style={{ ...styles.h5, fontSize: HEADING_SIZES[5] }}>{children}</h5>,
          h6: ({ children }) => <h6 style={{ ...styles.h6, fontSize: HEADING_SIZES[6] }}>{children}</h6>,
          p: ({ children }) => <p style={styles.p}>{children}</p>,
          ul: ({ children }) => <ul style={styles.ul}>{children}</ul>,
          ol: ({ children }) => <ol style={styles.ol}>{children}</ol>,
          li: ({ children }) => <li style={styles.li}>{children}</li>,
          blockquote: ({ children }) => <blockquote style={styles.blockquote}>{children}</blockquote>,
          code: ({ className, children }) => {
            // 带 language- 类名的是代码块内层 code；无类名为行内 code。
            if (className) {
              return <code className={className}>{children}</code>
            }
            return <code style={styles.code}>{children}</code>
          },
          pre: ({ children }) => <pre style={styles.pre}>{children}</pre>,
          table: ({ children }) => <table style={styles.table}>{children}</table>,
          th: ({ children }) => <th style={styles.th}>{children}</th>,
          td: ({ children }) => <td style={styles.td}>{children}</td>,
          tr: ({ children }) => <tr style={styles.tr}>{children}</tr>,
          hr: () => <hr style={styles.hr} />,
          a: ({ href, children }) => (
            <a href={href} target="_blank" rel="noreferrer" style={styles.a}>
              {children}
            </a>
          ),
          img: ({ src, alt }) => <img src={src} alt={alt ?? ''} style={styles.img} />,
        }}
      >
        {text}
      </ReactMarkdown>
    </div>
  )
}

/** Markdown 单行摘要（列表行内展示，纯文本截断，不渲染块级元素）。 */
export function MarkdownInlineSummary({ text, max = 80 }: { text: string; max?: number }) {
  const plain = text
    .replace(/```[\s\S]*?```/g, ' ')
    .replace(/`([^`]*)`/g, '$1')
    .replace(/!\[[^\]]*\]\([^)]*\)/g, ' ')
    .replace(/\[([^\]]*)\]\([^)]*\)/g, '$1')
    .replace(/[#>*_~|-]+/g, ' ')
    .replace(/\s+/g, ' ')
    .trim()
  return (
    <Typography.Text ellipsis style={{ maxWidth: 360 }}>
      {plain.length > max ? `${plain.slice(0, max)}…` : plain || '（空内容）'}
    </Typography.Text>
  )
}
