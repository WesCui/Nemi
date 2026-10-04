import type { Metadata } from "next";
import "./globals.css";

export const metadata: Metadata = {
  title: "Nemi · 你的生活助理",
  description: "把想做的事交给妮米，一件一件，安排妥当。",
  robots: { index: false, follow: false },
};
export default function Layout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="zh-CN">
      <body>{children}</body>
    </html>
  );
}
