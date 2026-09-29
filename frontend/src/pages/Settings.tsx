import { useState } from "react";
import axios from "axios";
import useSWR from "swr";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Card, CardContent, CardDescription, CardHeader, CardTitle, CardFooter } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Button } from "@/components/ui/button";
import { useToast } from "@/hooks/use-toast";
import { Save, PlugZap, MessageSquareText, Image as ImageIcon, Search } from "lucide-react";
import { motion } from "framer-motion";

import { api, fetcher } from "@/lib/api";

type ConfigType = {
  provider?: string;
  base_url?: string;
  api_key?: string;
  model?: string;
  reasoning_effort?: string;
  [key: string]: string | undefined;
};

type AnswerConfigType = {
  mode?: string;
  temperature?: number;
  system_prompt?: string;
  [key: string]: string | number | undefined;
};

type SettingsData = {
  text: ConfigType;
  vision: ConfigType;
  answer: AnswerConfigType;
  text_providers: string[];
  vision_providers: string[];
};

function errorDescription(err: unknown) {
  return axios.isAxiosError<{ error?: string }>(err) ? err.response?.data?.error || err.message : "未知错误";
}

export default function Settings() {
  const { data, error, isLoading, mutate } = useSWR<SettingsData>("/api/settings", fetcher);

  if (isLoading) return <div className="p-8 flex items-center text-muted-foreground animate-pulse">加载配置中...</div>;
  if (error) return <div className="p-8 text-destructive">加载配置失败</div>;

  return (
    <motion.div initial={false} animate={{ opacity: 1, y: 0 }} transition={{ duration: 0.5 }} className="w-full min-w-0 space-y-6 max-w-4xl">
      <div>
        <h2 className="text-3xl font-bold tracking-tight text-foreground">系统设置</h2>
        <p className="text-muted-foreground mt-1 text-sm">配置大语言模型、视觉模型以及底层搜题引擎的 API 密钥及端点。</p>
      </div>

      <Tabs defaultValue="text" className="w-full min-w-0">
        <TabsList className="grid w-full min-w-0 grid-cols-3 mb-6 p-1 bg-muted/50 rounded-xl">
          <TabsTrigger value="text" className="min-w-0 px-1 text-xs sm:px-3 sm:text-sm rounded-lg data-[state=active]:shadow-sm"><MessageSquareText className="hidden sm:block w-4 h-4 mr-2" />文本大模型</TabsTrigger>
          <TabsTrigger value="vision" className="min-w-0 px-1 text-xs sm:px-3 sm:text-sm rounded-lg data-[state=active]:shadow-sm"><ImageIcon className="hidden sm:block w-4 h-4 mr-2" />视觉模型</TabsTrigger>
          <TabsTrigger value="answer" className="min-w-0 px-1 text-xs sm:px-3 sm:text-sm rounded-lg data-[state=active]:shadow-sm"><Search className="hidden sm:block w-4 h-4 mr-2" />答题设置</TabsTrigger>
        </TabsList>

        <TabsContent value="text" className="focus-visible:outline-none focus-visible:ring-0">
          <ConfigForm
            key={JSON.stringify(data?.text)}
            type="text"
            title="文本大模型配置 (Text Model)"
            description="用于提取纯文本题目和选项的结构化答案。推荐使用 DeepSeek、OpenAI 等标准接口。"
            initialData={data?.text}
            providers={data?.text_providers || []}
            showTest={true}
            onSaved={mutate}
          />
        </TabsContent>
        <TabsContent value="vision" className="focus-visible:outline-none focus-visible:ring-0">
          <ConfigForm
            key={JSON.stringify(data?.vision)}
            type="vision"
            title="视觉模型配置 (Vision Model)"
            description="当题目包含图片时，调用支持视觉能力的大模型（如 GPT-4o, Qwen-VL）进行图片阅读与答题。"
            initialData={data?.vision}
            providers={data?.vision_providers || []}
            showTest={true}
            onSaved={mutate}
          />
        </TabsContent>
        <TabsContent value="answer" className="focus-visible:outline-none focus-visible:ring-0">
          <AnswerConfigForm
            key={JSON.stringify(data?.answer)}
            initialData={data?.answer}
            onSaved={mutate}
          />
        </TabsContent>
      </Tabs>
    </motion.div>
  );
}

function ConfigForm({
  type,
  title,
  description,
  initialData,
  providers,
  showTest,
  onSaved
}: {
  type: string;
  title: string;
  description: string;
  initialData?: ConfigType;
  providers: string[];
  showTest: boolean;
  onSaved: () => void;
}) {
  const [formData, setFormData] = useState<ConfigType>(initialData || {});
  const [isSaving, setIsSaving] = useState(false);
  const [isTesting, setIsTesting] = useState(false);
  const { toast } = useToast();

  const handleChange = (e: React.ChangeEvent<HTMLInputElement | HTMLSelectElement>) => {
    setFormData(prev => ({ ...prev, [e.target.name]: e.target.value }));
  };

  const handleSave = async () => {
    setIsSaving(true);
    try {
      await api.put(`/api/settings/${type}`, formData);
      toast({
        title: "保存成功",
        description: "配置信息已成功更新并生效。",
        className: "bg-emerald-50 text-emerald-900 border-emerald-200 dark:bg-emerald-950 dark:text-emerald-100 dark:border-emerald-900",
      });
      onSaved();
    } catch (err: unknown) {
      toast({
        variant: "destructive",
        title: "保存失败",
        description: errorDescription(err),
      });
    } finally {
      setIsSaving(false);
    }
  };

  const handleTest = async () => {
    setIsTesting(true);
    try {
      const res = await api.get(`/api/test-${type}`);
      if (res.data?.ok === false) {
        toast({
          variant: "destructive",
          title: "测试失败",
          description: res.data?.error || "未知错误",
        });
        return;
      }
      toast({
        title: "连通性测试通过",
        description: "模型可以正常连接并返回期望的结果。",
        className: "bg-emerald-50 text-emerald-900 border-emerald-200 dark:bg-emerald-950 dark:text-emerald-100 dark:border-emerald-900",
      });
    } catch (err: unknown) {
      toast({
        variant: "destructive",
        title: "测试失败",
        description: axios.isAxiosError<{ error?: string }>(err) ? err.response?.data?.error || err.message : "未知错误",
      });
    } finally {
      setIsTesting(false);
    }
  };

  const showReasoningEffort = type === "text" && formData.provider === "deepseek";

  return (
    <Card className="glass border-border/50 shadow-sm">
      <CardHeader className="border-b border-border/40 pb-6 bg-muted/20">
        <CardTitle className="text-xl flex items-center gap-2">{title}</CardTitle>
        <CardDescription className="text-sm mt-1">{description}</CardDescription>
      </CardHeader>
      <CardContent className="space-y-6 pt-6">
        <div className="grid gap-2">
          <Label htmlFor="provider" className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">Provider</Label>
          <select
            id="provider"
            name="provider"
            value={formData.provider || ''}
            onChange={handleChange}
            className="flex h-10 w-full rounded-md border border-input bg-background/50 px-3 py-2 text-sm ring-offset-background focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
          >
            <option value="">(未配置)</option>
            {providers.map(p => (
              <option key={p} value={p}>{p}</option>
            ))}
          </select>
          <p className="text-[11px] text-muted-foreground">选择模型提供方，不同 provider 对应不同的接口适配。</p>
        </div>
        <div className="grid gap-2">
          <Label htmlFor="base_url" className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">API Base URL</Label>
          <Input
            id="base_url"
            name="base_url"
            value={formData.base_url || ''}
            onChange={handleChange}
            placeholder="https://api.openai.com/v1"
            className="bg-background/50 focus-visible:ring-primary"
          />
          <p className="text-[11px] text-muted-foreground">必须包含协议头 (如 https://) 和具体的端点路径。</p>
        </div>
        <div className="grid gap-2">
          <Label htmlFor="api_key" className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">API Key</Label>
          <Input
            id="api_key"
            name="api_key"
            type="password"
            value={formData.api_key || ''}
            onChange={handleChange}
            placeholder="sk-..."
            className="bg-background/50 focus-visible:ring-primary font-mono"
          />
          <p className="text-[11px] text-muted-foreground">请妥善保管您的密钥，系统将使用此密钥进行鉴权。</p>
        </div>
        <div className="grid gap-2">
          <Label htmlFor="model" className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">模型名称 (Model Name)</Label>
          <Input
            id="model"
            name="model"
            value={formData.model || ''}
            onChange={handleChange}
            placeholder="gpt-4o / deepseek-chat"
            className="bg-background/50 focus-visible:ring-primary"
          />
          <p className="text-[11px] text-muted-foreground">指定请求底层大模型时使用的具体模型 ID。</p>
        </div>
        {showReasoningEffort && (
          <div className="grid gap-2">
            <Label htmlFor="reasoning_effort" className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">推理强度</Label>
            <select
              id="reasoning_effort"
              name="reasoning_effort"
              value={formData.reasoning_effort || 'max'}
              onChange={handleChange}
              className="flex h-10 w-full rounded-md border border-input bg-background/50 px-3 py-2 text-sm ring-offset-background focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
            >
              <option value="high">high</option>
              <option value="max">max</option>
            </select>
            <p className="text-[11px] text-muted-foreground">仅 DeepSeek provider 支持，控制推理链的深度。</p>
          </div>
        )}
      </CardContent>
      <CardFooter className="flex justify-between items-center border-t border-border/40 bg-muted/10 pt-4">
        <div>
          {showTest && (
            <Button variant="outline" onClick={handleTest} disabled={isTesting || isSaving} className="shadow-sm">
              <PlugZap className={`w-4 h-4 mr-2 ${isTesting ? 'animate-pulse text-blue-500' : 'text-muted-foreground'}`} />
              {isTesting ? "测试中..." : "测试连通性"}
            </Button>
          )}
        </div>
        <Button onClick={handleSave} disabled={isSaving || isTesting} className="shadow-sm">
          <Save className="w-4 h-4 mr-2" />
          {isSaving ? "保存中..." : "保存配置"}
        </Button>
      </CardFooter>
    </Card>
  );
}

function AnswerConfigForm({
  initialData,
  onSaved
}: {
  initialData?: AnswerConfigType;
  onSaved: () => void;
}) {
  const [formData, setFormData] = useState<AnswerConfigType>(initialData || {});
  const [isSaving, setIsSaving] = useState(false);
  const { toast } = useToast();

  const handleChange = (e: React.ChangeEvent<HTMLInputElement | HTMLSelectElement | HTMLTextAreaElement>) => {
    const val = e.target.name === "temperature" ? parseFloat(e.target.value) || 0 : e.target.value;
    setFormData(prev => ({ ...prev, [e.target.name]: val }));
  };

  const handleSave = async () => {
    setIsSaving(true);
    try {
      await api.put(`/api/settings/answer`, formData);
      toast({
        title: "保存成功",
        description: "答题设置已成功更新并生效。",
        className: "bg-emerald-50 text-emerald-900 border-emerald-200 dark:bg-emerald-950 dark:text-emerald-100 dark:border-emerald-900",
      });
      onSaved();
    } catch (err: unknown) {
      toast({
        variant: "destructive",
        title: "保存失败",
        description: errorDescription(err),
      });
    } finally {
      setIsSaving(false);
    }
  };

  return (
    <Card className="glass border-border/50 shadow-sm">
      <CardHeader className="border-b border-border/40 pb-6 bg-muted/20">
        <CardTitle className="text-xl flex items-center gap-2">答题设置 (Answer Config)</CardTitle>
        <CardDescription className="text-sm mt-1">配置答题模式、System Prompt 和温度参数。</CardDescription>
      </CardHeader>
      <CardContent className="space-y-6 pt-6">
        <div className="grid gap-2">
          <Label htmlFor="mode" className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">答题模式</Label>
          <select
            id="mode"
            name="mode"
            value={formData.mode || 'stepwise'}
            onChange={handleChange}
            className="flex h-10 w-full rounded-md border border-input bg-background/50 px-3 py-2 text-sm ring-offset-background focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
          >
            <option value="stepwise">Stepwise (逐张VL → 文本LLM)</option>
            <option value="direct">Direct (全部图片 → VL直接答题)</option>
          </select>
          <p className="text-[11px] text-muted-foreground">Stepwise 先用视觉模型识别图片再用文本模型答题；Direct 直接把图片和题目一起发给视觉模型。</p>
        </div>
        <div className="grid gap-2">
          <Label htmlFor="system_prompt" className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">System Prompt</Label>
          <textarea
            id="system_prompt"
            name="system_prompt"
            value={formData.system_prompt || ''}
            onChange={handleChange}
            rows={6}
            className="flex w-full rounded-md border border-input bg-background/50 px-3 py-2 text-sm ring-offset-background focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 font-mono"
          />
          <p className="text-[11px] text-muted-foreground">引导模型输出 &lt;answer&gt; 标签的系统提示词。</p>
        </div>
        <div className="grid gap-2">
          <Label htmlFor="temperature" className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">Temperature</Label>
          <Input
            id="temperature"
            name="temperature"
            type="number"
            step="0.1"
            min="0"
            max="2"
            value={formData.temperature ?? 0.1}
            onChange={handleChange}
            className="bg-background/50 focus-visible:ring-primary"
          />
          <p className="text-[11px] text-muted-foreground">控制输出随机性，0 = 完全确定性，1 = 较有创造性。</p>
        </div>
      </CardContent>
      <CardFooter className="flex justify-end items-center border-t border-border/40 bg-muted/10 pt-4">
        <Button onClick={handleSave} disabled={isSaving} className="shadow-sm">
          <Save className="w-4 h-4 mr-2" />
          {isSaving ? "保存中..." : "保存配置"}
        </Button>
      </CardFooter>
    </Card>
  );
}
