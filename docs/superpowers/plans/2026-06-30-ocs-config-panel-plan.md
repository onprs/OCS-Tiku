# OCS Config Panel Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a collapsible UI panel to the dashboard that fetches the OCS question bank configuration JSON from the backend and allows one-click copying.

**Architecture:** A single React component modification in `Dashboard.tsx`. We will use existing hooks (`useSWR` for fetching, `useToast` for notifications), existing UI components (`Card`, `Button`), and `framer-motion` (already used in the file) for smooth collapse/expand transitions.

**Tech Stack:** React, TailwindCSS, framer-motion, SWR.

---

### Task 1: Add the Config Panel to Dashboard

**Files:**
- Modify: `frontend/src/pages/Dashboard.tsx`

- [ ] **Step 1: Add new imports**
We need `useState`, `ChevronDown`, `Copy`, `useToast` and the `Button` component.

In `frontend/src/pages/Dashboard.tsx`, modify the imports at the top:

```tsx
import { useState } from 'react';
import useSWR from 'swr';
import { Activity, Database, CheckCircle, TrendingUp, ChevronDown, Copy } from 'lucide-react';
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from '@/components/ui/card';
import { AreaChart, Area, XAxis, YAxis, CartesianGrid, Tooltip, ResponsiveContainer } from 'recharts';
import { fetcher } from '@/lib/api';
import { motion, AnimatePresence } from 'framer-motion';
import { Button } from '@/components/ui/button';
import { useToast } from '@/hooks/use-toast';
```

- [ ] **Step 2: Add state and fetch hook to `Dashboard` component**
Inside `Dashboard()`, right below the existing `useSWR` calls, add the state and config fetching:

```tsx
  const { data: stats, error: statsError, isLoading: statsLoading } = useSWR('/api/stats', fetcher);
  const { data: dashboardData, error: dashboardError, isLoading: dashboardLoading } = useSWR('/api/dashboard', fetcher);
  
  // New hooks for OCS Config Panel
  const { data: configData } = useSWR('/api/config', fetcher);
  const [configExpanded, setConfigExpanded] = useState(false);
  const { toast } = useToast();

  const handleCopyConfig = (e: React.MouseEvent) => {
    e.stopPropagation();
    if (!configData) return;
    const jsonStr = JSON.stringify(configData, null, 2);
    navigator.clipboard.writeText(jsonStr).then(() => {
      toast({
        title: "复制成功",
        description: "配置已复制到剪贴板，请前往 OCS 脚本粘贴。",
      });
    }).catch(() => {
      toast({
        title: "复制失败",
        description: "请手动展开并复制配置内容。",
        variant: "destructive",
      });
    });
  };
```

- [ ] **Step 3: Insert the collapsible Card in the render tree**
Place the new `Card` exactly between the grid of 3 metric cards and the chart.

Find this block:
```tsx
      </motion.div>

      <motion.div variants={itemVariants} initial="hidden" animate="show" transition={{ delay: 0.3 }}>
        <Card className="glass border-border/50 hover:shadow-md transition-shadow duration-300">
```

Modify it to include the new Config Panel `Card`:
```tsx
      </motion.div>

      {/* OCS Config Panel */}
      <motion.div variants={itemVariants} initial="hidden" animate="show" transition={{ delay: 0.25 }}>
        <Card className="glass border-border/50 hover:shadow-md transition-all duration-300 overflow-hidden">
          <CardHeader 
            className="flex flex-row items-center justify-between cursor-pointer bg-muted/30 hover:bg-muted/50 transition-colors py-4"
            onClick={() => setConfigExpanded(!configExpanded)}
          >
            <div className="flex items-center gap-2">
              <span className="text-xl">🚀</span>
              <CardTitle className="text-base sm:text-lg">OCS 题库配置 (点击展开/收起)</CardTitle>
              <motion.div
                animate={{ rotate: configExpanded ? 180 : 0 }}
                transition={{ duration: 0.3 }}
              >
                <ChevronDown className="h-5 w-5 text-muted-foreground" />
              </motion.div>
            </div>
            <Button 
              variant="default" 
              size="sm" 
              className="h-8 gap-1.5"
              onClick={handleCopyConfig}
            >
              <Copy className="h-4 w-4" />
              一键复制
            </Button>
          </CardHeader>
          <AnimatePresence initial={false}>
            {configExpanded && (
              <motion.section
                key="content"
                initial="collapsed"
                animate="open"
                exit="collapsed"
                variants={{
                  open: { opacity: 1, height: "auto" },
                  collapsed: { opacity: 0, height: 0 }
                }}
                transition={{ duration: 0.3, ease: "easeInOut" }}
              >
                <CardContent className="pt-4 border-t border-border/50">
                  <div className="bg-muted/50 rounded-lg p-4 font-mono text-sm text-foreground overflow-x-auto whitespace-pre">
                    {configData ? JSON.stringify(configData, null, 2) : "加载中..."}
                  </div>
                  <div className="mt-3 text-sm text-muted-foreground flex items-center gap-2">
                    <span className="text-amber-500">💡</span> 
                    复制后，打开 OCS 脚本悬浮窗 → 通用 → 全局设置 → 题库配置，解析器选择「默认」，粘贴并保存即可。
                  </div>
                </CardContent>
              </motion.section>
            )}
          </AnimatePresence>
        </Card>
      </motion.div>

      <motion.div variants={itemVariants} initial="hidden" animate="show" transition={{ delay: 0.3 }}>
        <Card className="glass border-border/50 hover:shadow-md transition-shadow duration-300">
```

- [ ] **Step 4: Commit changes**
```bash
git add frontend/src/pages/Dashboard.tsx
git commit -m "feat: add OCS config panel to dashboard with copy-to-clipboard"
```
