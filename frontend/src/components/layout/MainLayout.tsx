import { Outlet, Link, useLocation } from "react-router-dom";
import { Suspense } from "react";
import { LayoutDashboard, FileText, Settings, Sparkles } from "lucide-react";
import { motion } from "framer-motion";
import {
  Sidebar,
  SidebarContent,
  SidebarGroup,
  SidebarGroupContent,
  SidebarGroupLabel,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarProvider,
  SidebarTrigger,
  SidebarHeader,
} from "@/components/ui/sidebar";

const items = [
  {
    title: "数据看板 (Dashboard)",
    url: "/",
    icon: LayoutDashboard,
  },
  {
    title: "题库管理 (Records)",
    url: "/records",
    icon: FileText,
  },
  {
    title: "引擎设置 (Settings)",
    url: "/settings",
    icon: Settings,
  },
];

export function MainLayout() {
  const location = useLocation();

  return (
    <SidebarProvider>
      <div className="flex min-h-screen w-full relative z-0 overflow-hidden bg-background">
        
        {/* Rich Background Elements */}
        <div className="absolute inset-0 z-[-1] pointer-events-none">
          <div className="absolute inset-0 bg-grid-pattern opacity-50"></div>
          <div className="absolute -top-[20%] -left-[10%] w-[50%] h-[50%] rounded-full bg-blue-500/10 blur-[120px] animate-pulse"></div>
          <div className="absolute bottom-[-20%] right-[-10%] w-[50%] h-[50%] rounded-full bg-violet-500/10 blur-[120px] animate-pulse" style={{ animationDelay: '2s' }}></div>
        </div>

        <Sidebar variant="sidebar" className="border-r border-border/50 glass z-20">
          <SidebarHeader className="p-4 pt-6">
            <div className="flex items-center gap-2 px-2 font-bold text-lg tracking-tight text-primary">
              <motion.div 
                initial={{ rotate: -180, scale: 0 }}
                animate={{ rotate: 0, scale: 1 }}
                transition={{ type: "spring", stiffness: 260, damping: 20 }}
                className="flex h-8 w-8 items-center justify-center rounded-lg bg-gradient-to-br from-blue-500 to-violet-600 text-white shadow-md"
              >
                <Sparkles className="h-4 w-4" />
              </motion.div>
              <span className="bg-clip-text text-transparent bg-gradient-to-r from-blue-600 to-violet-600 dark:from-blue-400 dark:to-violet-400">
                OCS 题库引擎
              </span>
            </div>
          </SidebarHeader>
          <SidebarContent className="px-2 mt-4">
            <SidebarGroup>
              <SidebarGroupLabel className="px-2 text-xs font-semibold text-muted-foreground uppercase tracking-wider">
                功能导航
              </SidebarGroupLabel>
              <SidebarGroupContent className="mt-2">
                <SidebarMenu>
                  {items.map((item) => {
                    const isActive = location.pathname === item.url || (item.url !== "/" && location.pathname.startsWith(item.url));
                    return (
                      <SidebarMenuItem key={item.title}>
                        <SidebarMenuButton asChild isActive={isActive} tooltip={item.title} className="transition-all duration-300 overflow-hidden relative group">
                          <Link to={item.url} className="flex items-center gap-3 w-full">
                            {isActive && (
                              <motion.div 
                                layoutId="sidebar-active" 
                                className="absolute left-0 w-1 h-6 bg-primary rounded-r-md" 
                                transition={{ type: "spring", stiffness: 300, damping: 30 }}
                              />
                            )}
                            <item.icon className={`h-4 w-4 z-10 relative transition-transform duration-300 group-hover:scale-110 ${isActive ? 'text-primary' : 'text-muted-foreground'}`} />
                            <span className={`z-10 relative ${isActive ? 'font-medium text-foreground' : 'text-muted-foreground'}`}>
                              {item.title}
                            </span>
                          </Link>
                        </SidebarMenuButton>
                      </SidebarMenuItem>
                    );
                  })}
                </SidebarMenu>
              </SidebarGroupContent>
            </SidebarGroup>
          </SidebarContent>
        </Sidebar>
        
        <div className="flex-1 flex flex-col min-w-0 z-10 relative">
          <header className="sticky top-0 z-30 flex h-16 items-center gap-4 border-b border-border/40 glass px-6 shadow-sm backdrop-blur-xl">
            <SidebarTrigger className="text-muted-foreground hover:text-foreground transition-colors" />
            <div className="flex-1"></div>
            {/* Can add user profile/theme toggle here later */}
          </header>
          
          <main className="flex-1 p-6 lg:p-8 overflow-x-hidden overflow-y-auto">
            <div className="mx-auto max-w-6xl">
              <Suspense fallback={<div role="status" className="p-8 text-sm text-muted-foreground">加载中...</div>}>
                <Outlet />
              </Suspense>
            </div>
          </main>
        </div>
      </div>
    </SidebarProvider>
  );
}
