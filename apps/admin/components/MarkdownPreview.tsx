"use client";

import ReactMarkdown, { defaultUrlTransform } from "react-markdown";

import { previewUrlTransform, rehypePlugins, remarkPlugins, youtubeIds } from "@/lib/markdown-preview.mjs";

const YOUTUBE_ID = /^[A-Za-z0-9_-]{11}$/;

function YouTube({ id, title }: { id: string; title: string }) {
  if (!YOUTUBE_ID.test(id)) return null;
  // 利用者が書いたiframeは通さない。動画IDからアプリ側で埋め込みを作る（D10）
  return (
    <div className="embed">
      <iframe
        src={`https://www.youtube-nocookie.com/embed/${id}`}
        title={title}
        loading="lazy"
        referrerPolicy="no-referrer"
        sandbox="allow-scripts allow-same-origin allow-presentation"
        allowFullScreen
      />
    </div>
  );
}

/**
 * 下書きのプレビュー。**描画時にrehype-sanitizeを通す**（lib/markdown-preview.mjs。サーバーの検査を通っていない入力も描画するため）。
 * 保存前の入力もそのまま描画するので、ここが保存型・反射型のXSSの防壁になる。
 */
export default function MarkdownPreview({ markdown }: { markdown: string }) {
  // 本文中の埋め込み（data-youtube）と同じ動画は、関連動画に重ねて出さない
  const inline = new Set([...markdown.matchAll(/data-youtube="([A-Za-z0-9_-]{11})"/g)].map((m) => m[1]));
  const related = youtubeIds(markdown);
  return (
    <div className="preview" data-testid="preview">
      <ReactMarkdown
        remarkPlugins={remarkPlugins}
        rehypePlugins={rehypePlugins}
        urlTransform={(url) => previewUrlTransform(defaultUrlTransform(url))}
        components={{
          div: ({ node, children }) => {
            const p = node?.properties ?? {};
            if (typeof p.dataYoutube === "string") {
              return <YouTube id={p.dataYoutube} title="YouTube動画" />;
            }
            if (p.dataVideo === "featured") return <p className="hint">（ここに紹介動画が入ります。公開サイトで表示されます）</p>;
            if (p.dataCarousel === "overview") return <p className="hint">（ここに画像のカルーセルが入ります。公開サイトで表示されます）</p>;
            return <div>{children}</div>;
          },
          a: ({ href, children }) => (
            <a href={href} rel="noopener noreferrer" target={href?.startsWith("http") ? "_blank" : undefined}>
              {children}
            </a>
          ),
          // eslint-disable-next-line @next/next/no-img-element
          img: ({ src, alt }) => <img src={typeof src === "string" ? src : undefined} alt={alt ?? ""} loading="lazy" />,
        }}
      >
        {markdown}
      </ReactMarkdown>
      {related.filter((id) => !inline.has(id)).map((id) => (
        <YouTube key={id} id={id} title="本文のリンクの動画" />
      ))}
    </div>
  );
}
