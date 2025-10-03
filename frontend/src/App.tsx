import mermaid from "mermaid"
import {
  FileUp,
  LoaderCircle,
  MessageSquare,
  PanelLeft,
  Send,
  Settings2,
  Sparkles,
} from "lucide-react"
import { useEffect, useId, useRef, useState } from "react"

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Select } from "@/components/ui/select"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { Textarea } from "@/components/ui/textarea"

type SidebarView = "chat" | "settings"

type Session = {
  session_id: string
  status: string
  uploaded_file_count: number
  table_count: number
  import_task_count: number
  total_row_count: number
}

type Settings = {
  provider: string
  model: string
  base_url: string
  api_key: string
  embedding_provider: string
  embedding_model: string
  embedding_base_url: string
  embedding_api_key: string
  default_chart_mode: "data" | "mermaid" | "mcp"
  mcp_server_url: string
}

type PlannedFilter = {
  column: string
  operator: string
  value: string
}

type QueryPlan = {
  source_table: string
  source_file: string
  source_sheet: string
  candidate_tables: string[]
  planning_confidence: number
  selection_reason: string
  dimension_column: string
  metric_column: string
  time_column: string
  selected_columns: string[]
  filters: string[]
  planned_filters: PlannedFilter[]
  question: string
  chart_type: string
  mode: string
  sql: string
}

type QueryResponse = {
  summary: string
  sql: string
  rows: Record<string, unknown>[]
  columns: string[]
  chart_mode: "data" | "mermaid" | "mcp"
  chart?: Record<string, unknown>
  visualization?: Record<string, unknown>
  query_plan?: QueryPlan
  warnings?: string[]
  row_count: number
  executed: boolean
}

type ChatUploadResponse = {
  session_id: string
  session_created: boolean
  import?: {
    task_id: string
    status: string
    file_count: number
    file_names: string[]
    warning_count: number
    warnings: string[]
  }
  answer?: QueryResponse
}

type ChatQueryResponse = {
  session_id: string
  answer: QueryResponse
}

type Message =
  | {
      id: string
      role: "user"
      content: string
      attachments?: string[]
    }
  | {
      id: string
      role: "system"
      content: string
    }
  | {
      id: string
      role: "assistant"
      content: QueryResponse
    }

const initialSettings: Settings = {
  provider: "",
  model: "",
  base_url: "",
  api_key: "",
  embedding_provider: "",
  embedding_model: "",
  embedding_base_url: "",
  embedding_api_key: "",
  default_chart_mode: "data",
  mcp_server_url: "http://chart-mcp:1122/mcp",
}

mermaid.initialize({
  startOnLoad: false,
  theme: "neutral",
  securityLevel: "loose",
})

function App() {
  const [sidebarView, setSidebarView] = useState<SidebarView>("chat")
  const [sessions, setSessions] = useState<Session[]>([])
  const [selectedSessionId, setSelectedSessionId] = useState("")
  const [settings, setSettings] = useState<Settings>(initialSettings)
  const [globalStatus, setGlobalStatus] = useState<Record<string, unknown>>({})
  const [question, setQuestion] = useState("")
  const [chartMode, setChartMode] = useState<"data" | "mermaid" | "mcp">("data")
  const [messagesBySession, setMessagesBySession] = useState<Record<string, Message[]>>({})
  const [pendingFiles, setPendingFiles] = useState<File[]>([])
  const [busy, setBusy] = useState(false)
  const [statusText, setStatusText] = useState("准备就绪")
  const fileInputRef = useRef<HTMLInputElement | null>(null)
  const effectiveSessionID = selectedSessionId || sessions[0]?.session_id || ""

  const activeMessages = effectiveSessionID ? messagesBySession[effectiveSessionID] || [] : []
  const selectedSession = sessions.find((session) => session.session_id === effectiveSessionID)

  useEffect(() => {
    void boot()
  }, [])

  async function boot() {
    try {
      await Promise.all([loadStatus(), loadSettings(), loadSessions()])
      setStatusText("工作台已就绪")
    } catch (error) {
      setStatusText(asErrorMessage(error))
    }
  }

  async function request<T>(path: string, init?: RequestInit): Promise<T> {
    const response = await fetch(path, {
      headers: {
        ...(init?.body instanceof FormData ? {} : { "Content-Type": "application/json" }),
        ...(init?.headers || {}),
      },
      ...init,
    })
    const contentType = response.headers.get("content-type") || ""
    const body = contentType.includes("application/json") ? await response.json() : await response.text()
    if (!response.ok) {
      throw new Error(typeof body === "string" ? body : JSON.stringify(body))
    }
    return body as T
  }

  async function loadStatus() {
    const status = await request<Record<string, unknown>>("/api/status")
    setGlobalStatus(status)
  }

  async function loadSettings() {
    const next = await request<Settings>("/api/settings/model")
    setSettings(next)
    setChartMode(next.default_chart_mode || "data")
  }

  async function saveSettings() {
    setBusy(true)
    try {
      const next = await request<Settings>("/api/settings/model", {
        method: "PUT",
        body: JSON.stringify(settings),
      })
      setSettings(next)
      setStatusText("模型配置已保存")
    } catch (error) {
      setStatusText(asErrorMessage(error))
    } finally {
      setBusy(false)
    }
  }

  async function loadSessions() {
    const response = await request<{ sessions: Session[] }>("/api/sessions")
    setSessions(response.sessions || [])
    if (!selectedSessionId && response.sessions?.length) {
      setSelectedSessionId(response.sessions[0].session_id)
    }
  }

  function startNewConversation() {
    setSelectedSessionId("")
    setPendingFiles([])
    setQuestion("")
    setStatusText("新对话已准备，可直接附加文件后提问")
    setSidebarView("chat")
  }

  async function queueFiles(fileList: FileList | null) {
    if (!fileList?.length) {
      return
    }
    const files = Array.from(fileList)
    setPendingFiles((prev) => [...prev, ...files])
    setStatusText(`正在导入 ${files.length} 个文件...`)
    await uploadPendingFiles(files)
  }

  function removePendingFile(name: string) {
    setPendingFiles((prev) => prev.filter((file) => file.name !== name))
  }

  async function ask() {
    const content = question.trim()
    if (!content) {
      setStatusText("请输入问题")
      return
    }
    if (!effectiveSessionID && pendingFiles.length === 0) {
      setStatusText("新对话需要先附加文件，系统会自动创建会话")
      return
    }

    const attachmentNames = pendingFiles.map((file) => file.name)
    const sessionKey = effectiveSessionID || `draft-${Date.now()}`
    pushMessage(sessionKey, {
      id: nextMessageID(),
      role: "user",
      content,
      attachments: attachmentNames,
    })

    setQuestion("")
    setBusy(true)
    if (!selectedSessionId && effectiveSessionID) {
      setSelectedSessionId(effectiveSessionID)
    }
    setStatusText("正在查询...")

    try {
      if (pendingFiles.length > 0) {
        const form = new FormData()
        if (selectedSessionId) {
          form.append("session_id", selectedSessionId)
        }
        form.append("question", content)
        form.append("chart_mode", chartMode)
        pendingFiles.forEach((file) => form.append("file", file))

        const response = await request<ChatUploadResponse>("/api/chat/upload", {
          method: "POST",
          body: form,
        })

        const resolvedSessionID = response.session_id
        adoptMessages(sessionKey, resolvedSessionID)
        setSelectedSessionId(resolvedSessionID)
        setPendingFiles([])

        if (response.answer) {
          pushMessage(resolvedSessionID, {
            id: nextMessageID(),
            role: "assistant",
            content: response.answer,
          })
        }

        await Promise.all([loadSessions(), loadStatus()])
        setStatusText(
          response.import
            ? `已导入 ${response.import.file_count} 个文件并完成问答`
            : "上传成功",
        )
        return
      }

      const response = await request<ChatQueryResponse>("/api/chat/query", {
        method: "POST",
        body: JSON.stringify({
          session_id: effectiveSessionID,
          question: content,
          chart_mode: chartMode,
        }),
      })

      pushMessage(response.session_id, {
        id: nextMessageID(),
        role: "assistant",
        content: response.answer,
      })
      setStatusText("查询完成")
    } catch (error) {
      setStatusText(asErrorMessage(error))
    } finally {
      setBusy(false)
    }
  }

  async function uploadPendingFiles(files: File[]) {
    const sessionKey = selectedSessionId || `draft-${Date.now()}`
    setBusy(true)
    try {
      const form = new FormData()
      if (selectedSessionId) {
        form.append("session_id", selectedSessionId)
      }
      form.append("chart_mode", chartMode)
      files.forEach((file) => form.append("file", file))

      const response = await request<ChatUploadResponse>("/api/chat/upload", {
        method: "POST",
        body: form,
      })

      const resolvedSessionID = response.session_id
      adoptMessages(sessionKey, resolvedSessionID)
      setSelectedSessionId(resolvedSessionID)
      setPendingFiles([])
      pushMessage(resolvedSessionID, {
        id: nextMessageID(),
        role: "system",
        content: `已导入 ${response.import?.file_count || files.length} 个文件，会话 ${resolvedSessionID} 已就绪。`,
      })
      await Promise.all([loadSessions(), loadStatus()])
      setStatusText(`已导入 ${response.import?.file_count || files.length} 个文件`)
    } catch (error) {
      setStatusText(asErrorMessage(error))
    } finally {
      setBusy(false)
    }
  }

  function pushMessage(sessionID: string, message: Message) {
    setMessagesBySession((prev) => ({
      ...prev,
      [sessionID]: [...(prev[sessionID] || []), message],
    }))
  }

  function adoptMessages(fromSessionID: string, toSessionID: string) {
    if (fromSessionID === toSessionID) {
      return
    }
    setMessagesBySession((prev) => {
      const existing = prev[fromSessionID] || []
      const next = {
        ...prev,
        [toSessionID]: [...(prev[toSessionID] || []), ...existing],
      }
      delete next[fromSessionID]
      return next
    })
  }

  return (
    <div className="min-h-screen bg-[radial-gradient(circle_at_top_left,_rgba(255,244,214,0.8),_transparent_28%),linear-gradient(180deg,_#f5efe6_0%,_#efe7db_100%)] text-stone-900">
      <div className="flex min-h-screen">
        <aside className="w-full max-w-[320px] border-r border-white/60 bg-white/80 p-5 backdrop-blur-xl">
          <div className="flex items-start justify-between">
            <div>
              <div className="flex items-center gap-2 text-sm font-medium text-stone-500">
                <Sparkles className="size-4 text-orange-500" />
                Excel AI Analysis
              </div>
              <div className="mt-2 text-2xl font-semibold tracking-tight">Workspace</div>
              <div className="mt-1 text-sm text-stone-500">
                一个聊天式的表格分析工作台
              </div>
            </div>
            <div className="rounded-2xl border border-stone-200 bg-stone-50 p-2">
              <PanelLeft className="size-4 text-stone-500" />
            </div>
          </div>

          <div className="mt-6 grid gap-2">
            <SidebarButton
              active={sidebarView === "chat"}
              icon={<MessageSquare className="size-4" />}
              title="对话"
              subtitle="聊天、附件和历史会话"
              onClick={() => setSidebarView("chat")}
            />
            <SidebarButton
              active={sidebarView === "settings"}
              icon={<Settings2 className="size-4" />}
              title="设置"
              subtitle="模型和默认图表模式"
              onClick={() => setSidebarView("settings")}
            />
          </div>

          {sidebarView === "chat" ? (
            <div className="mt-6 grid h-[calc(100vh-240px)] grid-rows-[auto_1fr] gap-4">
              <Button className="justify-start rounded-2xl" onClick={startNewConversation} disabled={busy}>
                <MessageSquare className="mr-2 size-4" />
                新对话
              </Button>

              <ScrollArea className="pr-1">
                <div className="grid gap-3">
                  <div className="text-xs font-medium uppercase tracking-[0.18em] text-stone-400">
                    Conversations
                  </div>
                  {sessions.length === 0 ? (
                    <div className="rounded-3xl border border-dashed border-stone-200 bg-stone-50 p-4 text-sm text-stone-500">
                      还没有历史会话。直接在右侧附加文件并提问，系统会自动创建会话。
                    </div>
                  ) : (
                    sessions.map((session) => (
                      <button
                        key={session.session_id}
                        type="button"
                        onClick={() => {
                          setSelectedSessionId(session.session_id)
                          setSidebarView("chat")
                          setStatusText(`已切换到 ${session.session_id}`)
                        }}
                        className={`rounded-3xl border px-4 py-3 text-left transition ${
                          selectedSessionId === session.session_id
                            ? "border-orange-300 bg-orange-50 shadow-sm"
                            : "border-stone-200 bg-white hover:bg-stone-50"
                        }`}
                      >
                        <div className="flex items-center justify-between gap-3">
                          <div className="truncate text-sm font-medium">{session.session_id}</div>
                          <Badge className="bg-stone-100 text-stone-700">{session.status}</Badge>
                        </div>
                        <div className="mt-2 text-xs text-stone-500">
                          files {session.uploaded_file_count} · tables {session.table_count} · rows{" "}
                          {session.total_row_count}
                        </div>
                      </button>
                    ))
                  )}
                </div>
              </ScrollArea>
            </div>
          ) : (
            <div className="mt-6">
              <Card className="border-stone-200 bg-white/90 shadow-none">
                <CardHeader>
                  <CardTitle>模型设置</CardTitle>
                  <CardDescription>
                    配置 SQL 生成模型和 embedding 检索模型。MCP 已内置到服务端部署里，不需要在前端单独配置。
                  </CardDescription>
                </CardHeader>
                <CardContent className="grid gap-3">
                  <Input
                    placeholder="Provider"
                    value={settings.provider}
                    onChange={(event) =>
                      setSettings((prev) => ({ ...prev, provider: event.target.value }))
                    }
                  />
                  <Input
                    placeholder="Model"
                    value={settings.model}
                    onChange={(event) =>
                      setSettings((prev) => ({ ...prev, model: event.target.value }))
                    }
                  />
                  <Input
                    placeholder="Base URL"
                    value={settings.base_url}
                    onChange={(event) =>
                      setSettings((prev) => ({ ...prev, base_url: event.target.value }))
                    }
                  />
                  <Input
                    placeholder="API Key"
                    value={settings.api_key}
                    onChange={(event) =>
                      setSettings((prev) => ({ ...prev, api_key: event.target.value }))
                    }
                  />
                  <div className="mt-2 text-xs font-medium uppercase tracking-[0.18em] text-stone-400">
                    Embeddings
                  </div>
                  <Input
                    placeholder="Embedding Provider"
                    value={settings.embedding_provider}
                    onChange={(event) =>
                      setSettings((prev) => ({ ...prev, embedding_provider: event.target.value }))
                    }
                  />
                  <Input
                    placeholder="Embedding Model"
                    value={settings.embedding_model}
                    onChange={(event) =>
                      setSettings((prev) => ({ ...prev, embedding_model: event.target.value }))
                    }
                  />
                  <Input
                    placeholder="Embedding Base URL"
                    value={settings.embedding_base_url}
                    onChange={(event) =>
                      setSettings((prev) => ({ ...prev, embedding_base_url: event.target.value }))
                    }
                  />
                  <Input
                    placeholder="Embedding API Key"
                    value={settings.embedding_api_key}
                    onChange={(event) =>
                      setSettings((prev) => ({ ...prev, embedding_api_key: event.target.value }))
                    }
                  />
                  <Select
                    value={settings.default_chart_mode}
                    onChange={(event) =>
                      setSettings((prev) => ({
                        ...prev,
                        default_chart_mode: event.target.value as Settings["default_chart_mode"],
                      }))
                    }
                  >
                    <option value="data">data</option>
                    <option value="mermaid">mermaid</option>
                    <option value="mcp">mcp</option>
                  </Select>
                  <Button onClick={() => void saveSettings()} disabled={busy}>
                    保存设置
                  </Button>
                </CardContent>
              </Card>
            </div>
          )}
        </aside>

        <main className="flex min-w-0 flex-1 flex-col p-6">
          <div className="mb-5 flex flex-wrap items-center justify-between gap-3">
            <div>
              <div className="text-sm font-medium text-stone-500">Chat Analyst</div>
              <h1 className="text-3xl font-semibold tracking-tight">SaaS-style spreadsheet Q&A</h1>
              <p className="mt-1 text-sm text-stone-500">
                把文件作为对话附件发送，系统会自动创建会话并完成导入。
              </p>
            </div>

            <div className="flex flex-wrap gap-2">
              <Badge>sessions: {String(globalStatus.session_count ?? 0)}</Badge>
              <Badge>ready: {String(globalStatus.ready_session_count ?? 0)}</Badge>
              <Badge>files: {String(globalStatus.uploaded_file_count ?? 0)}</Badge>
              <Badge>tables: {String(globalStatus.imported_table_count ?? 0)}</Badge>
            </div>
          </div>

          <div className="grid min-h-0 flex-1 grid-rows-[auto_1fr_auto] gap-4">
            <Card className="rounded-[28px] border-white/80 bg-white/85 shadow-[0_20px_70px_rgba(28,25,23,0.08)]">
              <CardContent className="flex flex-wrap items-center justify-between gap-3 p-5">
                <div>
                  <div className="text-sm font-medium text-stone-900">
                    {selectedSession ? "当前会话已连接" : "等待首个文件上传"}
                  </div>
                  <div className="mt-1 text-sm text-stone-500">
                    {selectedSession
                      ? `${selectedSession.session_id} · ${selectedSession.table_count} tables · ${selectedSession.total_row_count} rows`
                      : "没有显式的创建 Session 按钮。上传文件时自动创建。"}
                  </div>
                </div>
                <div className="flex items-center gap-3">
                  <Select
                    value={chartMode}
                    onChange={(event) => setChartMode(event.target.value as "data" | "mermaid" | "mcp")}
                    className="min-w-[140px]"
                  >
                    <option value="data">data</option>
                    <option value="mermaid">mermaid</option>
                    <option value="mcp">mcp</option>
                  </Select>
                </div>
              </CardContent>
            </Card>

            <Card className="min-h-0 rounded-[32px] border-white/80 bg-white/90 shadow-[0_20px_70px_rgba(28,25,23,0.08)]">
              <CardContent className="h-full p-4">
                <ScrollArea className="h-[calc(100vh-320px)] rounded-[26px] bg-stone-50/80 p-4">
                  <div className="grid gap-4">
                    {activeMessages.length === 0 ? (
                      <EmptyState pendingFiles={pendingFiles} />
                    ) : (
                      activeMessages.map((message) =>
                        message.role === "user" ? (
                          <UserMessage key={message.id} message={message} />
                        ) : message.role === "system" ? (
                          <SystemMessage key={message.id} content={message.content} />
                        ) : (
                          <AssistantMessage key={message.id} response={message.content} />
                        ),
                      )
                    )}
                  </div>
                </ScrollArea>
              </CardContent>
            </Card>

            <Card className="rounded-[30px] border-white/80 bg-white/95 shadow-[0_20px_70px_rgba(28,25,23,0.08)]">
              <CardContent className="p-4">
                {pendingFiles.length > 0 ? (
                  <div className="mb-3 flex flex-wrap gap-2">
                    {pendingFiles.map((file) => (
                      <button
                        key={`${file.name}-${file.size}`}
                        type="button"
                        onClick={() => removePendingFile(file.name)}
                        className="inline-flex items-center gap-2 rounded-full border border-orange-200 bg-orange-50 px-3 py-1 text-xs text-orange-800"
                      >
                        <FileUp className="size-3.5" />
                        {file.name}
                        <span className="text-orange-500">×</span>
                      </button>
                    ))}
                  </div>
                ) : null}

                <div className="grid gap-3 md:grid-cols-[1fr_auto]">
                  <Textarea
                    className="min-h-[120px] rounded-[24px] border-stone-200 bg-stone-50 px-4 py-4"
                    placeholder="问业务问题，或者先附加文件再发送。没有单独上传区，也不需要手动创建 Session。"
                    value={question}
                    onChange={(event) => setQuestion(event.target.value)}
                  />

                  <div className="grid gap-3">
                    <input
                      ref={fileInputRef}
                      className="hidden"
                      type="file"
                      multiple
                      onChange={(event) => {
                        void queueFiles(event.target.files).catch((error) =>
                          setStatusText(asErrorMessage(error)),
                        )
                        event.target.value = ""
                      }}
                    />
                    <Button
                      variant="outline"
                      className="h-11 justify-start rounded-2xl"
                      onClick={() => fileInputRef.current?.click()}
                      disabled={busy}
                    >
                      <FileUp className="mr-2 size-4" />
                      附加文件
                    </Button>
                    <Button className="h-11 justify-start rounded-2xl" onClick={() => void ask()} disabled={busy}>
                      {busy ? (
                        <LoaderCircle className="mr-2 size-4 animate-spin" />
                      ) : (
                        <Send className="mr-2 size-4" />
                      )}
                      发送
                    </Button>
                  </div>
                </div>

                <div className="mt-3 text-sm text-stone-500">{statusText}</div>
              </CardContent>
            </Card>
          </div>
        </main>
      </div>
    </div>
  )
}

function SidebarButton({
  active,
  icon,
  title,
  subtitle,
  onClick,
}: {
  active: boolean
  icon: React.ReactNode
  title: string
  subtitle: string
  onClick: () => void
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={`flex items-start gap-3 rounded-3xl border px-4 py-3 text-left transition ${
        active
          ? "border-orange-300 bg-orange-50 text-stone-950 shadow-sm"
          : "border-stone-200 bg-white text-stone-700 hover:bg-stone-50"
      }`}
    >
      <div className="mt-0.5 rounded-2xl border border-current/10 bg-white/80 p-2">{icon}</div>
      <div>
        <div className="font-medium">{title}</div>
        <div className="mt-1 text-sm text-stone-500">{subtitle}</div>
      </div>
    </button>
  )
}

function EmptyState({ pendingFiles }: { pendingFiles: File[] }) {
  return (
    <div className="grid place-items-center rounded-[28px] border border-dashed border-stone-300 bg-white p-10 text-center">
      <div className="max-w-xl">
        <div className="mx-auto mb-4 inline-flex rounded-full border border-orange-200 bg-orange-50 p-3 text-orange-600">
          <Sparkles className="size-5" />
        </div>
        <h2 className="text-xl font-semibold tracking-tight">直接通过聊天开始分析</h2>
        <p className="mt-2 text-sm leading-6 text-stone-500">
          不需要创建 Session，也没有独立上传面板。把文件作为消息附件发送，系统会自动创建会话、导入数据并返回 SQL、表格和图表。
        </p>
        {pendingFiles.length > 0 ? (
          <div className="mt-4 text-sm text-stone-600">
            当前已附加 {pendingFiles.length} 个文件，发送后会自动导入。
          </div>
        ) : null}
      </div>
    </div>
  )
}

function UserMessage({ message }: { message: Extract<Message, { role: "user" }> }) {
  return (
    <div className="ml-auto max-w-[78%] rounded-[28px] bg-stone-950 px-4 py-3 text-sm text-white shadow-sm">
      <div>{message.content}</div>
      {message.attachments?.length ? (
        <div className="mt-3 flex flex-wrap gap-2 text-xs text-stone-300">
          {message.attachments.map((item) => (
            <span
              key={item}
              className="inline-flex items-center gap-1 rounded-full border border-white/10 bg-white/5 px-2 py-1"
            >
              <FileUp className="size-3" />
              {item}
            </span>
          ))}
        </div>
      ) : null}
    </div>
  )
}

function SystemMessage({ content }: { content: string }) {
  return (
    <div className="mx-auto max-w-[75%] rounded-full border border-stone-200 bg-stone-100 px-4 py-2 text-center text-xs text-stone-600">
      {content}
    </div>
  )
}

function DataChart({
  rows,
  columns,
  chartType,
  xKey,
  yKey,
}: {
  rows: Record<string, unknown>[]
  columns: string[]
  chartType: string
  xKey: string
  yKey: string
}) {
  if (rows.length === 0 || columns.length === 0) {
    return <div className="text-xs text-stone-500">没有可渲染的数据。</div>
  }

  const points = rows.slice(0, 8).map((row, index) => {
    const x = String(row[xKey] ?? row[columns[0]] ?? `item-${index}`)
    const y = Number(row[yKey] ?? row[columns[columns.length - 1]] ?? 0)
    return { x, y }
  })
  const max = Math.max(...points.map((point) => point.y), 1)
  const total = points.reduce((sum, point) => sum + point.y, 0)

  if (chartType === "pie") {
    return (
      <div className="grid gap-2">
        {points.map((point) => (
          <div key={point.x} className="grid gap-1">
            <div className="flex items-center justify-between text-xs text-stone-600">
              <span>{point.x}</span>
              <span>{total > 0 ? `${Math.round((point.y / total) * 100)}%` : "0%"}</span>
            </div>
            <div className="h-3 overflow-hidden rounded-full bg-stone-200">
              <div
                className="h-full rounded-full bg-gradient-to-r from-amber-500 to-orange-400"
                style={{ width: `${total > 0 ? (point.y / total) * 100 : 0}%` }}
              />
            </div>
          </div>
        ))}
      </div>
    )
  }

  if (chartType === "line") {
    return (
      <div className="grid gap-2">
        <div className="flex items-end gap-2">
          {points.map((point) => (
            <div key={point.x} className="flex min-w-0 flex-1 flex-col items-center gap-2">
              <div className="text-[10px] text-stone-500">{point.y}</div>
              <div className="flex h-28 w-full items-end rounded-2xl bg-white px-2 py-2">
                <div
                  className="w-full rounded-xl bg-gradient-to-t from-sky-500 to-cyan-300"
                  style={{ height: `${(point.y / max) * 100}%` }}
                />
              </div>
              <div className="max-w-full truncate text-[10px] text-stone-500">{point.x}</div>
            </div>
          ))}
        </div>
      </div>
    )
  }

  if (chartType === "bar") {
    return (
      <div className="grid gap-3">
        {points.map((point) => (
          <div key={point.x} className="grid gap-1">
            <div className="text-xs text-stone-600">
              {point.x} · {point.y}
            </div>
            <div className="h-3 overflow-hidden rounded-full bg-stone-200">
              <div
                className="h-full rounded-full bg-gradient-to-r from-orange-500 to-amber-400"
                style={{ width: `${(point.y / max) * 100}%` }}
              />
            </div>
          </div>
        ))}
      </div>
    )
  }

  return <div className="text-xs text-stone-500">当前结果更适合表格展示。</div>
}

function AssistantMessage({ response }: { response: QueryResponse }) {
  const rows = response.rows || []
  const columns = response.columns || []
  const chart = (response.chart || {}) as Record<string, unknown>
  const visualization = (response.visualization || {}) as Record<string, unknown>
  const queryPlan = response.query_plan
  const mcpResult = (chart.result || {}) as Record<string, unknown>
  const mcpURL =
    typeof chart.url === "string" ? chart.url : typeof mcpResult.url === "string" ? mcpResult.url : ""
  const mcpExecuted = typeof chart.executed === "boolean" ? chart.executed : false
  const mcpTool = String(chart.tool || mcpResult.tool_name || "")
  const mcpError = String(chart.error || "")

  return (
    <div className="grid gap-3 rounded-[28px] border border-stone-200 bg-white p-4 shadow-sm">
      <div className="text-sm font-medium text-stone-900">{response.summary}</div>
      <div className="text-xs text-stone-500">
        mode: {response.chart_mode} · executed: {String(response.executed)} · rows: {response.row_count}
      </div>
      {queryPlan ? (
        <div className="grid gap-3 rounded-2xl border border-stone-200 bg-stone-50 p-3">
          <div className="flex flex-wrap gap-2 text-xs">
            <Badge className="bg-orange-100 text-orange-700">intent: {queryPlan.mode}</Badge>
            <Badge className="bg-sky-100 text-sky-700">chart: {queryPlan.chart_type || "table"}</Badge>
            <Badge className="bg-emerald-100 text-emerald-700">
              confidence: {Math.round((queryPlan.planning_confidence || 0) * 100)}%
            </Badge>
          </div>
          <div className="grid gap-1 text-xs text-stone-600">
            <div>
              source: {queryPlan.source_table || "-"}
              {queryPlan.source_file ? ` · ${queryPlan.source_file}` : ""}
              {queryPlan.source_sheet ? ` · ${queryPlan.source_sheet}` : ""}
            </div>
            <div>reason: {queryPlan.selection_reason || "-"}</div>
            {queryPlan.candidate_tables?.length ? (
              <div>candidates: {queryPlan.candidate_tables.join(", ")}</div>
            ) : null}
          </div>
        </div>
      ) : null}
      <pre className="overflow-auto rounded-2xl bg-stone-950 p-4 text-xs text-stone-100">{response.sql}</pre>

      <div className="rounded-[24px] border border-stone-200 bg-stone-50 p-4">
        {chart.mode === "mermaid" ? (
          <MermaidChart content={String(chart.content || "")} />
        ) : chart.mode === "mcp" ? (
          <div className="grid gap-3">
            <div className="flex flex-wrap gap-2 text-xs">
              <Badge className={mcpExecuted ? "bg-emerald-100 text-emerald-700" : "bg-stone-200 text-stone-700"}>
                executed: {String(mcpExecuted)}
              </Badge>
              {mcpTool ? <Badge className="bg-orange-100 text-orange-700">{mcpTool}</Badge> : null}
              {typeof chart.endpoint === "string" && chart.endpoint ? (
                <Badge className="bg-sky-100 text-sky-700">{String(chart.endpoint)}</Badge>
              ) : null}
            </div>

            {mcpURL ? (
              <div className="grid gap-2">
                <a
                  className="text-xs font-medium text-orange-700 underline underline-offset-2"
                  href={mcpURL}
                  target="_blank"
                  rel="noreferrer"
                >
                  Open rendered chart
                </a>
                <img
                  src={mcpURL}
                  alt="Rendered MCP chart"
                  className="max-h-[420px] rounded-2xl border border-stone-200 bg-white object-contain"
                />
              </div>
            ) : null}

            {mcpError ? (
              <div className="rounded-xl border border-red-200 bg-red-50 p-3 text-xs text-red-700">
                {mcpError}
              </div>
            ) : null}

            <pre className="overflow-auto rounded-xl bg-white p-3 text-xs text-stone-700">
              {JSON.stringify(chart, null, 2)}
            </pre>
          </div>
        ) : (
          <DataChart
            rows={rows}
            columns={columns}
            chartType={String(visualization.type || queryPlan?.chart_type || "table")}
            xKey={String(visualization.x || columns[0] || "")}
            yKey={String(visualization.y || columns[columns.length - 1] || "")}
          />
        )}
      </div>

      {columns.length > 0 ? (
        <div className="overflow-hidden rounded-2xl border border-stone-200">
          <Table>
            <TableHeader>
              <TableRow>
                {columns.map((column) => (
                  <TableHead key={column}>{column}</TableHead>
                ))}
              </TableRow>
            </TableHeader>
            <TableBody>
              {rows.map((row, index) => (
                <TableRow key={index}>
                  {columns.map((column) => (
                    <TableCell key={column}>{String(row[column] ?? "")}</TableCell>
                  ))}
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      ) : null}

      {response.warnings?.length ? (
        <div className="flex flex-wrap gap-2">
          {response.warnings.map((warning, index) => (
            <Badge key={index} className="bg-stone-200 text-stone-700">
              {warning}
            </Badge>
          ))}
        </div>
      ) : null}
    </div>
  )
}

function MermaidChart({ content }: { content: string }) {
  const [svg, setSVG] = useState("")
  const [error, setError] = useState("")
  const id = useId().replace(/[^a-zA-Z0-9_-]/g, "")

  useEffect(() => {
    let active = true

    async function render() {
      if (!content.trim()) {
        setSVG("")
        setError("")
        return
      }

      try {
        const result = await mermaid.render(`mermaid-${id}`, content)
        if (!active) return
        setSVG(result.svg)
        setError("")
      } catch (renderError) {
        if (!active) return
        setSVG("")
        setError(asErrorMessage(renderError))
      }
    }

    void render()

    return () => {
      active = false
    }
  }, [content, id])

  if (error) {
    return (
      <div className="grid gap-2">
        <div className="text-xs font-medium text-red-600">Mermaid 渲染失败</div>
        <pre className="overflow-auto rounded-xl bg-white p-3 text-xs text-stone-700">{content}</pre>
        <div className="text-xs text-stone-500">{error}</div>
      </div>
    )
  }

  if (!svg) {
    return <div className="text-xs text-stone-500">正在渲染 Mermaid 图表...</div>
  }

  return <div className="overflow-auto rounded-xl bg-white p-3" dangerouslySetInnerHTML={{ __html: svg }} />
}

function asErrorMessage(error: unknown) {
  if (error instanceof Error) return error.message
  return String(error)
}

function nextMessageID() {
  const maybeCrypto = globalThis.crypto as Crypto | undefined
  if (maybeCrypto && typeof maybeCrypto.randomUUID === "function") {
    return maybeCrypto.randomUUID()
  }
  return `msg-${Date.now()}-${Math.random().toString(16).slice(2)}`
}

export default App
