"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";

const NAV = [
  { href: "/playbooks", label: "Playbooks" },
  { href: "/investigations/new", label: "Start investigation" },
];

export function ConsoleHeader() {
  const pathname = usePathname();

  return (
    <header className="sticky top-0 z-20 border-b border-rule bg-panel">
      <div className="mx-auto flex w-full max-w-[1440px] flex-wrap items-center gap-x-6 gap-y-2 px-4 py-2.5 sm:px-6 lg:px-8">
        <Link href="/playbooks" className="flex items-baseline gap-2">
          <span className="font-mono text-[13px] font-medium tracking-[0.16em] text-ink">INTERBELLUM</span>
          <span className="label">Investigation console</span>
        </Link>

        <nav className="flex items-center gap-1" aria-label="Main">
          {NAV.map((item) => {
            const active = pathname === item.href || pathname.startsWith(`${item.href}/`);
            return (
              <Link
                key={item.href}
                href={item.href}
                aria-current={active ? "page" : undefined}
                className={`rounded-[2px] px-2.5 py-1 text-[13px] transition-colors ${
                  active
                    ? "bg-signal-soft font-medium text-signal"
                    : "text-ink-muted hover:bg-sunken hover:text-ink"
                }`}
              >
                {item.label}
              </Link>
            );
          })}
        </nav>

        <p className="ml-auto hidden text-[11px] text-ink-faint lg:block">
          A client of the same API an automated agent calls. Nothing here is a private endpoint.
        </p>
      </div>
    </header>
  );
}
