import type { Metadata } from "next";
import "./globals.css";

export const metadata: Metadata = {
  title: "Nemi · 你的生活助理",
  description: "事项、提醒与常用应用，你的个人生活空间。",
  robots: { index: false, follow: false },
};
export default function Layout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="zh-CN">
      <body>{children}</body>
    </html>
  );
}
