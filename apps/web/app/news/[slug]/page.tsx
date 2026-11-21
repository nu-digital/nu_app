import type { Metadata } from "next";
import { notFound } from "next/navigation";
import ContentArticle from "@/components/content/ContentArticle";
import { siteConfig } from "@/data/site";
import { getContentBySlug } from "@/lib/markdown";

type Props = {
  params: Promise<{ slug: string }>;
};

// 公開API（撤回・個人情報削除を次のリクエストから反映する）から描画時に取得する。ビルド時に焼き込まない
export const dynamic = "force-dynamic";

export async function generateMetadata({ params }: Props): Promise<Metadata> {
  const { slug } = await params;
  const post = await getContentBySlug("news", slug);

  if (!post) {
    return {};
  }

  const imageUrl = post.image.startsWith("http")
    ? post.image
    : `${siteConfig.url}${post.image}`;

  return {
    title: post.title,
    description: post.excerpt,
    openGraph: {
      title: post.title,
      description: post.excerpt,
      url: `${siteConfig.url}/news/${slug}`,
      images: [{ url: imageUrl, width: 1200, height: 630, alt: post.title }],
      type: "article",
    },
    twitter: {
      card: "summary_large_image",
      title: post.title,
      description: post.excerpt,
      images: [imageUrl],
    },
  };
}

export default async function NewsPostPage({ params }: Props) {
  const { slug } = await params;
  const post = await getContentBySlug("news", slug);

  if (!post) {
    notFound();
  }

  return <ContentArticle post={post} />;
}
