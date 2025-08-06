import type { HTMLAttributes } from "react"

import { cn } from "@/lib/utils"

export function Badge({ className, ...props }: HTMLAttributes<HTMLDivElement>) {
  return (
    <div
      className={cn(
        "inline-flex items-center rounded-full bg-orange-100 px-3 py-1 text-xs font-medium text-orange-900",
        className,
      )}
      {...props}
    />
  )
}
