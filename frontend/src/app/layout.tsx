import type { Metadata } from "next";
import { IBM_Plex_Mono, IBM_Plex_Sans } from "next/font/google";

import { ConsoleHeader } from "@/components/ConsoleHeader";

import "./globals.css";

// IBM Plex: drawn for machines and engineering documentation, and the sans and
// mono are one family — which is what lets every identifier, status chip and
// column head sit in the mono face without looking bolted on.
const plexSans = IBM_Plex_Sans({
  subsets: ["latin"],
  weight: ["400", "500", "600"],
  variable: "--font-plex-sans",
  display: "swap",
});

const plexMono = IBM_Plex_Mono({
  subsets: ["latin"],
  weight: ["400", "500"],
  variable: "--font-plex-mono",
  display: "swap",
});

export const metadata: Metadata = {
  title: "Interbellum Console",
  description: "Review console for the Interbellum alert investigation engine.",
};

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en" className={`${plexSans.variable} ${plexMono.variable}`}>
      <body className="min-h-full">
        <ConsoleHeader />
        <main className="mx-auto w-full max-w-[1440px] px-4 py-6 sm:px-6 lg:px-8">{children}</main>
      </body>
    </html>
  );
}
