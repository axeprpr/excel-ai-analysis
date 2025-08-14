import mermaid from "mermaid"
import { useEffect, useId, useMemo, useState } from "react"

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
import { Separator } from "@/components/ui/separator"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import { Textarea } from "@/components/ui/textarea"

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
  default_chart_mode: "data" | "mermaid" | "mcp"
  mcp_server_url: string
}

type QueryResponse = {
  summary: string
  sql: string
  rows: Record<string, unknown>[]
  columns: string[]
  chart_mode: "data" | "mermaid" | "mcp"
  chart?: Record<string, unknown>
  visualization?: Record<string, unknown>
  warnings?: string[]
  row_count: number
  executed: boolean
}

type Message =
  | { id: string; role: "user"; content: string }
  | { id: string; role: "assistant"; content: QueryResponse }

const initialSettings: Settings = {
  provider: "",
  model: "",
  base_url: "",
  api_key: "",
  default_chart_mode: "data",
  mcp_server_url: "http://chart-mcp:1122/mcp",
}

mermaid.initialize({
  startOnLoad: false,
  theme: "neutral",
  securityLevel: "loose",
})

function App() {
  const [sessions, setSessions] = useState<Session[]>([])
  const [selectedSessionId, setSelectedSessionId] = useState("")
  const [settings, setSettings] = useState<Settings>(initialSettings)
  const [globalStatus, setGlobalStatus] = useState<Record<string, unknown>>({})
  const [question, setQuestion] = useState("")
  const [chartMode, setChartMode] = useState<"data" | "mermaid" | "mcp">("data")
  const [messages, setMessages] = useState<Message[]>([])
  const [busy, setBusy] = useState(false)
  const [statusText, setStatusText] = useState("准备就绪")

  const selectedSession = useMemo(
    () => sessions.find((session) => session.session_id === selectedSessionId),
    [selectedSessionId, sessions],
  )

  useEffect(() => {
    void boot()
  }, [])

  async function boot() {
    try {
      await Promise.all([loadStatus(), loadSettings(), loadSessions()])
      setStatusText("前端已加载")
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
    const body = contentType.includes("application/json")
      ? await response.json()
      : await response.text()
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

  async function createSession() {
    setBusy(true)
    try {
      const session = await request<Session>("/api/sessions", { method: "POST" })
      await loadSessions()
      setSelectedSessionId(session.session_id)
      setStatusText(`已创建 ${session.session_id}`)
      await loadStatus()
    } finally {
      setBusy(false)
    }
  }

  async function uploadFiles(files: FileList | null) {
    if (!selectedSessionId) throw new Error("请先选择 Session")
    if (!files?.length) throw new Error("请选择上传文件")
    setBusy(true)
    try {
      const form = new FormData()
      Array.from(files).forEach((file) => form.append("file", file))
      const response = await request<{ task_id: string }>(
        `/api/sessions/${selectedSessionId}/files/upload`,
        {
          method: "POST",
          body: form,
        },
      )
      setStatusText(`上传成功，任务 ${response.task_id} 正在导入`)
      await waitForImportTask(response.task_id)
      await Promise.all([loadSessions(), loadStatus()])
    } finally {
      setBusy(false)
    }
  }

  async function waitForImportTask(taskId: string) {
    for (let i = 0; i < 60; i++) {
      const task = await request<{ status: string; error?: string }>(
        `/api/sessions/${selectedSessionId}/imports/${taskId}`,
      )
      if (task.status === "completed") {
        setStatusText(`导入完成，任务 ${taskId}`)
        return
      }
      if (task.status === "failed") {
        throw new Error(task.error || "导入失败")
      }
      await new Promise((resolve) => setTimeout(resolve, 1000))
    }
    throw new Error("导入等待超时")
  }

  async function ask() {
    if (!selectedSessionId) throw new Error("请先选择 Session")
    if (!question.trim()) throw new Error("请输入问题")
    const content = question.trim()
    setMessages((prev) => [...prev, { id: crypto.randomUUID(), role: "user", content }])
    setQuestion("")
    setBusy(true)
    try {
      const response = await request<QueryResponse>(`/api/sessions/${selectedSessionId}/query`, {
        method: "POST",
        body: JSON.stringify({
          question: content,
          chart_mode: chartMode,
        }),
      })
      setMessages((prev) => [
        ...prev,
        { id: crypto.randomUUID(), role: "assistant", content: response },
      ])
      setStatusText("查询完成")
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="min-h-screen bg-transparent text-stone-900">
      <div className="mx-auto max-w-[1500px] p-6">
        <div className="mb-6 grid gap-6 lg:grid-cols-[1.1fr_0.9fr]">
          <Card className="overflow-hidden bg-gradient-to-br from-white via-stone-50 to-orange-50">
            <CardHeader>
              <CardTitle className="text-3xl">Excel AI Analysis</CardTitle>
              <CardDescription>
                shadcn/ui 聊天工作台。上传 Excel，管理模型设置，并在对话消息里直接查看
                数据 / Mermaid / MCP 图表结果。
              </CardDescription>
            </CardHeader>
            <CardContent className="flex flex-wrap gap-2">
              <Badge>sessions: {String(globalStatus.session_count ?? 0)}</Badge>
              <Badge>ready: {String(globalStatus.ready_session_count ?? 0)}</Badge>
              <Badge>files: {String(globalStatus.uploaded_file_count ?? 0)}</Badge>
              <Badge>tables: {String(globalStatus.imported_table_count ?? 0)}</Badge>
            </CardContent>
          </Card>

          <Card>
            <CardHeader>
              <CardTitle>模型配置</CardTitle>
              <CardDescription>本地保存大模型与 MCP 图表服务配置。</CardDescription>
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
              <Input
                placeholder="MCP Server URL"
                value={settings.mcp_server_url}
                onChange={(event) =>
                  setSettings((prev) => ({ ...prev, mcp_server_url: event.target.value }))
                }
              />
              <Button onClick={() => void saveSettings()} disabled={busy}>
                保存配置
              </Button>
            </CardContent>
          </Card>
        </div>

        <div className="grid gap-6 lg:grid-cols-[320px_minmax(0,1fr)]">
          <Card className="h-[calc(100vh-220px)]">
            <CardHeader>
              <CardTitle>Sessions</CardTitle>
              <CardDescription>左侧管理会话、上传和当前数据范围。</CardDescription>
            </CardHeader>
            <CardContent className="grid h-[calc(100%-112px)] grid-rows-[auto_auto_1fr] gap-4">
              <Button onClick={() => void createSession()} disabled={busy}>
                创建 Session
              </Button>
              <div className="grid gap-3 rounded-2xl border border-stone-200 bg-stone-50 p-3">
                <div className="text-sm font-medium">上传文件到当前 Session</div>
                <input
                  type="file"
                  multiple
                  onChange={(event) => {
                    void uploadFiles(event.target.files).catch((error) =>
                      setStatusText(asErrorMessage(error)),
                    )
                  }}
                />
              </div>
              <ScrollArea className="grid gap-3 pr-1">
                <div className="grid gap-3">
                  {sessions.map((session) => (
                    <button
                      key={session.session_id}
                      type="button"
                      className={`rounded-2xl border p-4 text-left transition ${
                        selectedSessionId === session.session_id
                          ? "border-orange-400 bg-orange-50"
                          : "border-stone-200 bg-white hover:bg-stone-50"
                      }`}
                      onClick={() => setSelectedSessionId(session.session_id)}
                    >
                      <div className="font-medium">{session.session_id}</div>
                      <div className="mt-1 text-sm text-stone-500">{session.status}</div>
                      <div className="mt-2 text-xs text-stone-500">
                        files {session.uploaded_file_count} · tables {session.table_count} · rows{" "}
                        {session.total_row_count}
                      </div>
                    </button>
                  ))}
                </div>
              </ScrollArea>
            </CardContent>
          </Card>

          <Card className="h-[calc(100vh-220px)]">
            <CardHeader>
              <CardTitle>Chat Workspace</CardTitle>
              <CardDescription>
                当前会话：
                <span className="ml-1 font-medium">
                  {selectedSession?.session_id || "未选择"}
                </span>
              </CardDescription>
            </CardHeader>
            <CardContent className="grid h-[calc(100%-112px)] grid-rows-[1fr_auto] gap-4">
              <ScrollArea className="rounded-2xl border border-stone-200 bg-stone-50 p-4">
                <div className="grid gap-4">
                  {messages.length === 0 ? (
                    <div className="rounded-2xl border border-dashed border-stone-300 bg-white p-8 text-center text-sm text-stone-500">
                      还没有对话。先创建 Session、上传文件，然后在下方提问。
                    </div>
                  ) : (
                    messages.map((message) =>
                      message.role === "user" ? (
                        <div key={message.id} className="ml-auto max-w-[80%] rounded-3xl bg-stone-900 px-4 py-3 text-sm text-white">
                          {message.content}
                        </div>
                      ) : (
                        <AssistantMessage key={message.id} response={message.content} />
                      ),
                    )
                  )}
                </div>
              </ScrollArea>

              <div className="grid gap-3">
                <Separator />
                <div className="grid gap-3 md:grid-cols-[1fr_180px]">
                  <Textarea
                    placeholder="例如：按月展示销售趋势，顺便给我图表"
                    value={question}
                    onChange={(event) => setQuestion(event.target.value)}
                  />
                  <div className="grid gap-3">
                    <Select
                      value={chartMode}
                      onChange={(event) =>
                        setChartMode(event.target.value as "data" | "mermaid" | "mcp")
                      }
                    >
                      <option value="data">data</option>
                      <option value="mermaid">mermaid</option>
                      <option value="mcp">mcp</option>
                    </Select>
                    <Button onClick={() => void ask()} disabled={busy}>
                      发送问题
                    </Button>
                  </div>
                </div>
                <div className="text-sm text-stone-500">{statusText}</div>
              </div>
            </CardContent>
          </Card>
        </div>
      </div>
    </div>
  )
}

function AssistantMessage({ response }: { response: QueryResponse }) {
  const rows = response.rows || []
  const columns = response.columns || []
  const chart = (response.chart || {}) as Record<string, unknown>

  return (
    <div className="grid gap-3 rounded-3xl border border-stone-200 bg-white p-4 shadow-sm">
      <div className="text-sm font-medium text-stone-900">{response.summary}</div>
      <div className="text-xs text-stone-500">
        mode: {response.chart_mode} · executed: {String(response.executed)} · rows:{" "}
        {response.row_count}
      </div>
      <pre className="overflow-auto rounded-2xl bg-stone-950 p-4 text-xs text-stone-100">
        {response.sql}
      </pre>

      <div className="rounded-2xl border border-stone-200 bg-stone-50 p-4">
        {chart.mode === "mermaid" ? (
          <MermaidChart content={String(chart.content || "")} />
        ) : chart.mode === "mcp" ? (
          <pre className="overflow-auto text-xs text-stone-700">
            {JSON.stringify(chart, null, 2)}
          </pre>
        ) : (
          <div className="grid gap-3">
            {rows.slice(0, 8).map((row, index) => {
              const x = String(row[columns[0]] ?? `item-${index}`)
              const y = Number(row[columns[columns.length - 1]] ?? 0)
              const max = Math.max(...rows.map((item) => Number(item[columns[columns.length - 1]] ?? 0)), 1)
              return (
                <div key={`${x}-${index}`} className="grid gap-1">
                  <div className="text-xs text-stone-600">
                    {x} · {y}
                  </div>
                  <div className="h-3 overflow-hidden rounded-full bg-stone-200">
                    <div
                      className="h-full rounded-full bg-gradient-to-r from-orange-500 to-amber-400"
                      style={{ width: `${(y / max) * 100}%` }}
                    />
                  </div>
                </div>
              )
            })}
          </div>
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
        <pre className="overflow-auto rounded-xl bg-white p-3 text-xs text-stone-700">
          {content}
        </pre>
        <div className="text-xs text-stone-500">{error}</div>
      </div>
    )
  }

  if (!svg) {
    return <div className="text-xs text-stone-500">正在渲染 Mermaid 图表...</div>
  }

  return (
    <div
      className="overflow-auto rounded-xl bg-white p-3"
      dangerouslySetInnerHTML={{ __html: svg }}
    />
  )
}

function asErrorMessage(error: unknown) {
  if (error instanceof Error) return error.message
  return String(error)
}

export default App
