import React, { useEffect, useMemo, useRef, useState } from 'react'
import {
  Activity, AlertTriangle, BarChart3, Bot, CheckCircle2, ChevronDown, CircleDot,
  ClipboardList, Copy, Cpu, Database, ExternalLink, FolderGit2, Gauge, Home,
  Link2, ListTodo, LoaderCircle, Network, Play, Plus, RefreshCw, RotateCcw,
  Search, Settings, ShieldCheck, StopCircle, TerminalSquare, UserRound,
  Users, Workflow, XCircle, Zap,
} from 'lucide-react'
import {
  addProject, browseProjectFolders, cancelTask, connectionAction, createTask, feedbackTask, fmtDuration,
  fmtNumber, fmtTime, getProjects, getRuntime, getTaskFeedback, getTaskInspect,
  getTaskResult, getTasks, getTaskStatus, getUsage, inputTask,
  sandboxPrepare, updateProjectPolicy,
} from './api'

type View = 'overview' | 'live' | 'tasks' | 'workspaces' | 'connections' | 'usage' | 'diagnostics'
type TokenSample = { at: number; input: number; output: number }

const ACTIVE_STATES = new Set(['SUBMITTED','PREFLIGHT','WAITING_RESOURCE','WORKSPACE_READY','RUNNING','VERIFYING','REVIEWING','READY_TO_INTEGRATE','INTEGRATING','RETRY_WAIT'])
const TERMINAL_STATES = new Set(['COMPLETE','FAILED','CANCELLED','BLOCKED'])
const views: Array<{id: View; label: string; icon: React.ElementType}> = [
  { id: 'overview', label: 'Tổng quan', icon: Home },
  { id: 'live', label: 'Live Operations', icon: Activity },
  { id: 'tasks', label: 'Tasks', icon: ListTodo },
  { id: 'workspaces', label: 'Workspaces', icon: FolderGit2 },
  { id: 'connections', label: 'Connections', icon: Link2 },
  { id: 'usage', label: 'Usage', icon: BarChart3 },
  { id: 'diagnostics', label: 'Diagnostics', icon: Settings },
]

function stateText(state: string) {
  const s = String(state || '').toUpperCase()
  if (['SUBMITTED','PREFLIGHT','WAITING_RESOURCE','WORKSPACE_READY'].includes(s)) return 'Chuẩn bị'
  if (['RUNNING','RETRY_WAIT'].includes(s)) return 'Đang chạy'
  if (['VERIFYING','REVIEWING','READY_TO_INTEGRATE','INTEGRATING'].includes(s)) return 'Đang kiểm tra'
  if (s === 'INPUT_REQUIRED') return 'Cần input'
  if (s === 'COMPLETE') return 'Hoàn tất'
  if (s === 'CANCELLED') return 'Đã hủy'
  if (['BLOCKED','FAILED'].includes(s)) return 'Bị chặn'
  return s || 'Không rõ'
}
function stateTone(state: string) {
  const s = String(state || '').toUpperCase()
  if (s === 'COMPLETE') return 'success'
  if (s === 'INPUT_REQUIRED' || s === 'BLOCKED') return 'warning'
  if (s === 'FAILED') return 'danger'
  if (s === 'CANCELLED') return 'neutral'
  return 'info'
}
function isTaskActive(t: any) { return ACTIVE_STATES.has(String(t?.state || '').toUpperCase()) || !!t?.waiting_for_ai_turn }
function taskTitle(t: any) {
  const goal = String(t?.goal || '').trim()
  const first = goal.split(/\r?\n/).map((line: string)=>line.trim()).find(Boolean)
  return first || t?.id || 'Untitled task'
}
function connectionProvider(c: any) { return c?.id === 'claude-web' ? 'Claude' : 'GPT' }
function routeReady(c: any) {
  const s = String(c?.status || '').toUpperCase()
  return !!c && (c.connected === true || c.ready === true || c.route_ready === true || ['CONNECTED','LINK_READY','READY','AVAILABLE_LOCAL'].includes(s))
}
function connectionUsable(c: any) {
  if(!c)return false
  if(c.id==='openai-tunnel')return c.connected===true
  if(c.id==='chatgpt-web'||c.id==='claude-web')return c.usable_from_client===true
  return routeReady(c)
}
function connectionStageText(c:any){
  if(!c)return 'Không có dữ liệu'
  if(c.id==='openai-tunnel')return c.connected?'USABLE':(c.ready?'ROUTE_READY':String(c.status||'NOT_READY'))
  const stage=String(c.connection_stage||'').toUpperCase()
  return stage||String(c.status||'NOT_READY')
}
function actionableConnection(c: any) {
  return ['ERROR','MISCONFIGURED','DEGRADED','ROUTE_OFFLINE','BRIDGE_RUNTIME_MISSING','STABLE_URL_REQUIRED','REMOTE_BRIDGE_REQUIRED'].includes(String(c?.status || '').toUpperCase())
}
function operationalConnections(runtime: any) {
  const wanted = new Set(['openai-tunnel','chatgpt-web','claude-web'])
  return (runtime?.connections || []).filter((c: any) => wanted.has(c.id))
}
function primaryConnections(runtime: any) {
  const all = operationalConnections(runtime)
  const tunnel = all.find((c: any) => c.id === 'openai-tunnel')
  const fallback = all.find((c: any) => c.id === 'chatgpt-web')
  const claude = all.find((c: any) => c.id === 'claude-web')
  let gpt = tunnel || fallback
  if(connectionUsable(fallback))gpt=fallback
  if(connectionUsable(tunnel))gpt=tunnel
  if(!connectionUsable(gpt) && !routeReady(gpt) && routeReady(fallback))gpt=fallback
  return [gpt, claude].filter(Boolean)
}
function usageWindowValue(window: any) {
  if (!window) return '—'
  const measured = Number(window.results_with_token_data || 0)
  const results = Number(window.results || 0)
  if (measured > 0) return fmtNumber(window.total_tokens)
  return results > 0 ? '—' : '0'
}
function liveAggregate(tasks: any[]) {
  const active = tasks.filter(isTaskActive)
  return active.reduce((acc, t) => {
    const live = t.live_usage
    if (!live?.available) return acc
    acc.observable++
    if (live.pending_turn) acc.pending++
    if (live.tokens_available) {
      acc.input += Number(live.input_tokens || 0)
      acc.output += Number(live.output_tokens || 0)
      acc.total += Number(live.total_tokens || 0)
      acc.turns += Number(live.turns || 0)
      acc.tokenTasks++
    }
    return acc
  }, { active: active.length, observable: 0, pending: 0, input: 0, output: 0, total: 0, turns: 0, tokenTasks: 0 })
}
function systemHealth(runtime: any) {
  if (!runtime) return { label: 'Đang kết nối', tone: 'neutral', detail: 'Đang đọc runtime MAR.' }
  if (!runtime.sandbox_host_ready) return { label: 'Cần xử lý', tone: 'warning', detail: 'Sandbox Windows chưa sẵn sàng.' }
  const conns = primaryConnections(runtime)
  const problems = conns.filter(actionableConnection)
  if (problems.length) return { label: 'Suy giảm', tone: 'warning', detail: problems.map((c: any) => `${connectionProvider(c)}: ${c.status}`).join(' · ') }
  return { label: 'Hoạt động tốt', tone: 'success', detail: 'Sandbox và các route chính đang ổn định.' }
}

function StatusBadge({ state, children }: { state?: string; children?: React.ReactNode }) {
  return <span className={`status-badge ${stateTone(state || '')}`}><CircleDot size={12}/>{children || stateText(state || '')}</span>
}
function EmptyState({ icon: Icon = ClipboardList, title, text }: any) {
  return <div className="empty-state"><Icon size={34}/><strong>{title}</strong>{text && <p>{text}</p>}</div>
}
function IconButton({ icon: Icon, label, ...props }: any) {
  return <button className="icon-button" title={label} aria-label={label} {...props}><Icon size={17}/></button>
}

function TrendChart({ samples }: { samples: TokenSample[] }) {
  if (samples.length < 2) return <div className="chart-empty"><Activity size={30}/><span>Đang thu thập dữ liệu realtime…</span></div>
  const width = 820, height = 235, px = 34, py = 24
  const visible = samples.slice(-30)
  const max = Math.max(1, ...visible.flatMap(s => [s.input, s.output]))
  const points = (key: 'input'|'output') => visible.map((s, i) => {
    const x = px + i * (width - px * 2) / Math.max(1, visible.length - 1)
    const y = height - py - (s[key] / max) * (height - py * 2)
    return `${x.toFixed(1)},${y.toFixed(1)}`
  }).join(' ')
  return <svg className="trend-chart" viewBox={`0 0 ${width} ${height}`} preserveAspectRatio="none" role="img" aria-label="Realtime input and output token trend">
    {[.25,.5,.75,1].map(v => <line key={v} x1={px} x2={width-px} y1={height-py-v*(height-py*2)} y2={height-py-v*(height-py*2)} className="chart-grid-line"/>)}
    <polyline points={points('input')} className="chart-input-line"/>
    <polyline points={points('output')} className="chart-output-line"/>
  </svg>
}

function Shell({ view, setView, runtime, workspace, setWorkspace, projects, children }: any) {
  const health = systemHealth(runtime)
  return <div className="app-shell">
    <header className="topbar">
      <div className="brand-lockup"><div className="brand-mark">M</div><div><strong>MAR</strong><span>operations console</span></div></div>
      <div className="topbar-center"><label>Workspace<select id="workspace-scope-select" aria-label="Chọn workspace đang theo dõi" value={workspace} onChange={e=>setWorkspace(e.target.value)}><option value="">Tất cả workspaces</option>{projects.map((p:any)=><option key={p.id} value={p.id}>{p.id}</option>)}</select></label></div>
      <div className="runtime-pills">
        <span className={`runtime-pill ${runtime?'live':''}`}><span className="live-dot"/>Telemetry: {runtime?'live':'unavailable'}</span>
        <span className="runtime-pill"><Gauge size={14}/>Uptime: {fmtDuration(runtime?.uptime_seconds)}</span>
        <span className="runtime-pill"><Cpu size={14}/>{runtime?.model || 'model —'}</span>
        <span className="runtime-pill"><Users size={14}/>Workers: {runtime?.max_workers ?? '—'}</span>
        <div className="avatar"><UserRound size={18}/></div>
      </div>
    </header>
    <aside className="sidebar">
      <div className="sidebar-label">OPERATIONS</div>
      <nav>{views.map(item=>{const Icon=item.icon;return <button key={item.id} className={view===item.id?'active':''} onClick={()=>setView(item.id)}><Icon size={19}/><span>{item.label}</span></button>})}</nav>
      <div className={`sidebar-health ${health.tone}`}><span className="live-dot"/><div><strong>{health.label}</strong><small>{health.detail}</small></div></div>
      <div className="sidebar-version">React Owner Console</div>
    </aside>
    <main className="main-content">{children}</main>
  </div>
}

function LiveOperations({ runtime, tasks, usage, tokenSamples, setView, workspace }: any) {
  const scoped = workspace ? tasks.filter((t:any)=>t.project_id===workspace) : tasks
  const flows = scoped.filter(isTaskActive)
  const agg = liveAggregate(scoped)
  const health = systemHealth(runtime)
  const primary = primaryConnections(runtime)
  const connected = primary.filter(routeReady).length
  const routes = operationalConnections(runtime)
  const readyRoutes = routes.filter(routeReady).length
  const waitingAI = flows.filter((t:any)=>t.waiting_for_ai_turn).length
  return <>
    <div className="page-header"><div><div className="title-row"><h1>Live Operations</h1><span className="live-state"><span className="live-dot"/>Đang hoạt động</span></div><p>Giám sát realtime các luồng xử lý, kết nối và hiệu suất của MAR.</p></div><div className="last-update"><RefreshCw size={14}/>2 giây / lần</div></div>
    <section className="summary-cards four">
      <div className={`summary-card ${health.tone}`}><div className="summary-icon"><ShieldCheck size={25}/></div><div><span>Trạng thái hệ thống</span><strong>{health.label}</strong><small>{health.detail}</small></div></div>
      <div className="summary-card"><div className="summary-icon blue"><Database size={25}/></div><div><span>Kết nối AI</span><strong>{connected} / {primary.length}</strong><small>{primary.length ? 'Các provider chính' : 'Chưa có provider metadata'}</small></div></div>
      <div className="summary-card"><div className="summary-icon cyan"><Play size={25}/></div><div><span>Luồng đang hoạt động</span><strong>{flows.length}</strong><small>{flows.length ? 'Execution flow đang chạy' : 'Không có flow đang chạy'}</small></div></div>
      <div className="summary-card"><div className="summary-icon green"><Zap size={25}/></div><div><span>Token hôm nay</span><strong>{usageWindowValue(usage?.today)}</strong><small>{usage?.today ? `${fmtNumber(usage.today.input_tokens)} input · ${fmtNumber(usage.today.output_tokens)} output` : 'Đang tải usage'}</small></div></div>
    </section>
    <div className="live-main-grid">
      <section className="panel chart-panel"><div className="panel-heading"><div className="panel-title"><BarChart3 size={20}/><h2>Biểu đồ realtime</h2></div><div className="segmented"><span>Token input + output</span><ChevronDown size={14}/></div></div><div className="chart-kpis"><div><span>Live tokens</span><b>{agg.tokenTasks ? `~${fmtNumber(agg.total)}` : '—'}</b></div><div><span>Input</span><b>{agg.tokenTasks ? `~${fmtNumber(agg.input)}` : '—'}</b></div><div><span>Output</span><b>{agg.tokenTasks ? `~${fmtNumber(agg.output)}` : '—'}</b></div><div><span>Turns</span><b>{agg.observable ? fmtNumber(agg.turns) : '—'}</b></div></div><div className="chart-wrap"><TrendChart samples={tokenSamples}/></div><div className="chart-legend"><span><i className="blue"/>Input tokens</span><span><i className="green"/>Output tokens</span></div></section>
      <section className="panel active-panel"><div className="panel-heading"><div className="panel-title"><Workflow size={20}/><h2>Luồng đang hoạt động</h2></div><button className="text-button" onClick={()=>setView('tasks')}>Xem tất cả <ExternalLink size={14}/></button></div>{flows.length ? <div className="flow-list">{flows.slice(0,8).map((t:any)=><button key={t.id} className="flow-row" onClick={()=>setView('tasks')}><div><strong>{t.goal || t.id}</strong><small>{t.project_id} · epoch {t.run_epoch || 0}</small></div><StatusBadge state={t.state}/></button>)}</div> : <EmptyState icon={Workflow} title="Không có luồng nào đang hoạt động" text="Khi có flow chạy, thông tin sẽ hiện tại đây."/>}</section>
    </div>
    <div className="provider-and-stats"><div className="provider-grid">{['GPT','Claude'].map(provider=><ProviderZone key={provider} provider={provider} runtime={runtime}/>)}</div><section className="panel quick-panel"><div className="panel-heading"><div className="panel-title"><BarChart3 size={20}/><h2>Thống kê nhanh</h2></div></div><div className="quick-list"><Quick icon={Zap} label="Luồng hoạt động" value={String(flows.length)}/><Quick icon={LoaderCircle} label="Đang chờ AI" value={String(waitingAI)}/><Quick icon={Network} label="Tổng routes" value={`${readyRoutes} / ${routes.length}`}/><Quick icon={Database} label="Token hôm nay" value={usageWindowValue(usage?.today)}/></div></section></div>
  </>
}
function Quick({icon:Icon,label,value}:any){return <div className="quick-item"><div><Icon size={18}/><span>{label}</span></div><strong>{value}</strong></div>}
function ProviderZone({ provider, runtime }: any) {
  const conns = operationalConnections(runtime).filter((c:any)=>connectionProvider(c)===provider)
  const ready = conns.filter(routeReady).length
  const sessions = conns.reduce((n:number,c:any)=>n+(c.active_sessions_available?Number(c.active_sessions||0):0),0)
  const requests = conns.reduce((n:number,c:any)=>n+Number(c.requests||0),0)
  const isClaude = provider === 'Claude'
  return <section className={`panel provider-zone ${isClaude?'claude':'gpt'}`}><div className="provider-head"><div className={`provider-brand ${isClaude?'claude':''}`}><Bot size={24}/></div><div><h2>{provider}</h2><p>{isClaude?'Claude Web · Streamable HTTP':'GPT · Secure MCP Tunnel'}</p></div><span className={`connection-chip ${ready?'connected':'idle'}`}><span className="live-dot"/>{ready?'Đang kết nối':'Idle'}</span></div><div className="provider-metrics"><div><span>Routes</span><b>{ready}/{conns.length}</b></div><div><span>Sessions</span><b>{sessions || '—'}</b></div><div><span>Requests</span><b>{fmtNumber(requests)}</b></div></div><details><summary><ChevronDown size={15}/>Chi tiết bên dưới</summary><div className="route-list">{conns.length?conns.map((c:any)=><div className="route-row" key={c.id}><span>{c.name || c.id}</span><StatusBadge state={routeReady(c)?'RUNNING':(actionableConnection(c)?'FAILED':'CANCELLED')}/></div>):<span className="muted">Chưa có route.</span>}</div></details></section>
}

function taskProjectHead(t:any, projects:any[]){return String(projects.find((p:any)=>p.id===t.project_id)?.head||'').trim()}
function isHistoricalBlockedTask(t:any,projects:any[]){const state=String(t.state||'').toUpperCase();if(!['BLOCKED','FAILED'].includes(state))return false;const base=String(t.base_revision||'').trim(),head=taskProjectHead(t,projects);return !!base&&!!head&&base!==head}
function isActionableBlockedTask(t:any,projects:any[]){const state=String(t.state||'').toUpperCase();if(state==='INPUT_REQUIRED')return true;return ['BLOCKED','FAILED'].includes(state)&&!isHistoricalBlockedTask(t,projects)}

function Overview({ runtime, tasks, usage, setView, workspace, projects }: any) {
  const scoped = workspace ? tasks.filter((t:any)=>t.project_id===workspace) : tasks
  const active = scoped.filter(isTaskActive)
  const blocked = scoped.filter((t:any)=>isActionableBlockedTask(t,projects))
  const health = systemHealth(runtime)
  return <><div className="page-header"><div><h1>Tổng quan</h1><p>Ảnh chụp nhanh hệ thống MAR và công việc cần chú ý.</p></div><button className="primary-button" onClick={()=>setView('live')}><Activity size={17}/>Mở Live Operations</button></div><section className="summary-cards four"><div className={`summary-card ${health.tone}`}><div className="summary-icon"><ShieldCheck size={25}/></div><div><span>Hệ thống</span><strong>{health.label}</strong><small>{health.detail}</small></div></div><div className="summary-card"><div className="summary-icon blue"><Workflow size={25}/></div><div><span>Đang chạy</span><strong>{active.length}</strong><small>execution flows</small></div></div><div className="summary-card warning"><div className="summary-icon amber"><AlertTriangle size={25}/></div><div><span>Cần chú ý</span><strong>{blocked.length}</strong><small>blocked / input required</small></div></div><div className="summary-card"><div className="summary-icon green"><Database size={25}/></div><div><span>Token hôm nay</span><strong>{usageWindowValue(usage?.today)}</strong><small>MAR measured</small></div></div></section><div className="overview-grid"><section className="panel"><div className="panel-heading"><div className="panel-title"><Workflow size={20}/><h2>Current work</h2></div></div>{active.length?<div className="flow-list">{active.slice(0,8).map((t:any)=><div className="flow-row static" key={t.id}><div><strong>{t.goal}</strong><small>{t.project_id}</small></div><StatusBadge state={t.state}/></div>)}</div>:<EmptyState icon={Workflow} title="Không có task đang hoạt động"/>}</section><section className="panel"><div className="panel-heading"><div className="panel-title"><AlertTriangle size={20}/><h2>Action</h2></div></div>{blocked.length?<div className="flow-list">{blocked.slice(0,8).map((t:any)=><button className="flow-row" key={t.id} onClick={()=>setView('tasks')}><div><strong>{t.goal}</strong><small>{t.project_id}</small></div><StatusBadge state={t.state}/></button>)}</div>:<EmptyState icon={CheckCircle2} title="Không có việc cần Owner can thiệp"/>}</section></div></>
}

function TasksPage({ tasks, projects, workspace, reloadTasks }: any) {
  const scoped = workspace ? tasks.filter((t:any)=>t.project_id===workspace) : tasks
  const current = scoped.filter((t:any)=>!isHistoricalBlockedTask(t,projects))
  const [filter,setFilter]=useState('current'), [query,setQuery]=useState(''), [selected,setSelected]=useState<string>('')
  useEffect(()=>{if(!selected && scoped.length)setSelected((scoped.find((t:any)=>isActionableBlockedTask(t,projects))||current[0]||scoped[0]).id);if(selected&&!scoped.some((t:any)=>t.id===selected))setSelected('')},[tasks,workspace,projects])
  const filtered = scoped.filter((t:any)=>{const state=String(t.state||'').toUpperCase();const status=(filter==='current'&&!isHistoricalBlockedTask(t,projects))||filter==='all'||(filter==='active'&&(isTaskActive(t)||state==='INPUT_REQUIRED'))||(filter==='blocked'&&isActionableBlockedTask(t,projects))||(filter==='history'&&isHistoricalBlockedTask(t,projects))||(filter==='complete'&&state==='COMPLETE');const text=`${t.goal||''} ${t.id||''} ${t.project_id||''}`.toLowerCase();return status&&(!query||text.includes(query.toLowerCase()))})
  return <><div className="page-header"><div><h1>Tasks</h1><p>Theo dõi và quản lý các tác vụ AI trên toàn bộ workspaces.</p></div></div><div className="task-toolbar"><select value={filter} onChange={e=>setFilter(e.target.value)}><option value="current">Hiện tại · {current.length}</option><option value="blocked">Cần xử lý</option><option value="active">Đang chạy</option><option value="complete">Hoàn tất</option><option value="history">Lịch sử / superseded</option><option value="all">Tất cả · {scoped.length}</option></select><div className="search-box"><Search size={16}/><input value={query} onChange={e=>setQuery(e.target.value)} placeholder="Tìm task theo tiêu đề, id…"/></div></div><div className="task-three-col"><section className="panel task-list-panel"><div className="panel-heading"><div className="panel-title"><ListTodo size={19}/><h2>Danh sách task ({filtered.length})</h2></div><IconButton icon={RefreshCw} label="Làm mới" onClick={reloadTasks}/></div><div className="task-list">{filtered.length?filtered.map((t:any)=>{const historical=isHistoricalBlockedTask(t,projects);return <button className={`task-item ${selected===t.id?'selected':''}`} key={t.id} onClick={()=>setSelected(t.id)}><div className="task-item-top"><strong title={taskTitle(t)}>{taskTitle(t)}</strong>{historical?<StatusBadge state="CANCELLED">Lịch sử</StatusBadge>:<StatusBadge state={t.state}/>}</div><div className="task-item-meta"><span>{t.project_id}</span>{historical&&<span>Lịch sử / superseded</span>}<span>{t.live_usage?.tokens_available?`~${fmtNumber(t.live_usage.total_tokens)} tokens`:fmtNumber(t.usage?.model_total_tokens||0)+' tokens'}</span>{historical?<span>Lưu trữ</span>:<span>{fmtTime(t.updated_at)}</span>}</div></button>}):<EmptyState icon={ListTodo} title="Không có task phù hợp"/>}</div></section><TaskDetail id={selected} reloadTasks={reloadTasks} projects={projects}/><CreateTaskPanel projects={projects} preferredWorkspace={workspace} onCreated={reloadTasks}/></div></>
}
function TaskDetail({ id, reloadTasks, projects }: any) {
  const [data,setData]=useState<any>(null), [loading,setLoading]=useState(false), [input,setInput]=useState(''), [feedback,setFeedback]=useState('')
  const refresh=async()=>{if(!id)return;setLoading(true);try{const [s,r,i,f]=await Promise.allSettled([getTaskStatus(id),getTaskResult(id),getTaskInspect(id),getTaskFeedback(id)]);setData({status:s.status==='fulfilled'?s.value:null,result:r.status==='fulfilled'?r.value:null,inspect:i.status==='fulfilled'?i.value:null,feedback:f.status==='fulfilled'?f.value:null})}finally{setLoading(false)}}
  useEffect(()=>{refresh();if(!id)return;const timer=setInterval(refresh,5000);return()=>clearInterval(timer)},[id])
  if(!id)return <section className="panel task-detail-panel"><EmptyState icon={ClipboardList} title="Chọn một task" text="Trạng thái, result và evidence sẽ xuất hiện ở đây."/></section>
  if(!data)return <section className="panel task-detail-panel"><EmptyState icon={LoaderCircle} title={loading?'Đang tải task…':'Chưa có dữ liệu'}/></section>
  const s=data.status?.status||data.status||{}, task=s.task||{}, state=String(task.state||'').toUpperCase(), result=data.result?.result||null, feedbackRows=data.feedback?.feedback||[]
  const historical=isHistoricalBlockedTask({state,project_id:task.contract?.project_id,base_revision:task.contract?.base_revision},projects)
  const goal=task.contract?.goal||id, needsInput=!historical&&state==='INPUT_REQUIRED'&&!s.brain_turn_available
  return <section className="panel task-detail-panel"><div className="detail-head"><div><div className="eyebrow">{task.contract?.project_id||'workspace'}</div><h2>{goal}</h2><code>{id}</code></div>{historical?<StatusBadge state="CANCELLED">Lịch sử / superseded</StatusBadge>:<StatusBadge state={state}/>}</div><div className="detail-tabs"><button className="active">Tổng quan</button><button disabled>Nhật ký</button><button disabled>Tệp đính kèm</button></div><div className={`task-message ${needsInput?'warning':''}`}><strong>{historical?'Bản ghi lịch sử:':needsInput?'Bạn cần làm:':'Trạng thái kỹ thuật:'}</strong><span>{historical?'Task này thuộc revision cũ và được giữ lại để audit; không cần Owner xử lý.':needsInput?(s.next_action||s.detail||'MAR đang chờ thông tin của bạn.'):stateText(state)}</span></div>{result?<div className="result-grid"><Metric label="Verification" value={result.verdict||'—'}/><Metric label="Integration" value={result.integration_status||'—'}/><Metric label="Revision" value={String(result.final_revision||'—').slice(0,12)}/></div>:<div className="subtle-card">Chưa có durable result.</div>}{result&&<details className="evidence"><summary><ChevronDown size={15}/>Evidence / changed areas</summary><pre>{JSON.stringify({changed_areas:result.changed_areas,pass_fail_evidence:result.pass_fail_evidence,unresolved_risks:result.unresolved_risks},null,2)}</pre></details>}{needsInput&&<div className="input-stack"><textarea value={input} onChange={e=>setInput(e.target.value)} placeholder="Nhập phần thông tin/quyết định MAR đang cần…"/><button className="primary-button" onClick={async()=>{if(!input.trim())return;await inputTask(id,input.trim());setInput('');await refresh();await reloadTasks()}}>Gửi và tiếp tục</button></div>}<div className="detail-actions"><button onClick={refresh}><RefreshCw size={16}/>Làm mới</button>{!historical&&<button className="danger-button" disabled={TERMINAL_STATES.has(state)} onClick={async()=>{if(confirm('Hủy task này?')){await cancelTask(id);await refresh();await reloadTasks()}}}><XCircle size={16}/>Hủy task</button>}</div>{result&&<div className="feedback-box"><h3>Phản hồi của bạn</h3><textarea value={feedback} onChange={e=>setFeedback(e.target.value)} placeholder="Bạn thấy gì khi dùng thật?"/><div className="feedback-actions"><button onClick={async()=>{await feedbackTask(id,'ACCEPTED',feedback);setFeedback('');await refresh()}}><CheckCircle2 size={16}/>Chấp nhận</button><button onClick={async()=>{await feedbackTask(id,'REJECTED',feedback);setFeedback('');await refresh()}}><XCircle size={16}/>Chưa đạt</button><button onClick={async()=>{await feedbackTask(id,'COMMENT',feedback);setFeedback('');await refresh()}}>Ghi chú</button></div>{feedbackRows.length>0&&<div className="feedback-history">{feedbackRows.slice(0,5).map((f:any,i:number)=><div key={i}><b>{f.verdict}</b><span>{fmtTime(f.created_at)}</span><p>{f.message}</p></div>)}</div>}</div>}</section>
}
function Metric({label,value}:any){return <div className="metric-box"><span>{label}</span><b>{value}</b></div>}
function CreateTaskPanel({projects,preferredWorkspace,onCreated}:any){const [project,setProject]=useState(preferredWorkspace||''),[title,setTitle]=useState(''),[description,setDescription]=useState(''),[priority,setPriority]=useState('P2'),[verification,setVerification]=useState('go-standard'),[acceptance,setAcceptance]=useState(''),[busy,setBusy]=useState(false),[error,setError]=useState('');useEffect(()=>{if(preferredWorkspace)setProject(preferredWorkspace)},[preferredWorkspace]);const p=projects.find((x:any)=>x.id===project);async function submit(e:React.FormEvent){e.preventDefault();if(!p?.head||!title.trim())return;setBusy(true);setError('');try{const accepts=acceptance.split(/\r?\n/).map(x=>x.trim()).filter(Boolean);await createTask({project_id:project,base_revision:p.head,goal:description.trim()?`${title.trim()}\n\n${description.trim()}`:title.trim(),acceptance:accepts.length?accepts:['Hoàn thành đúng kết quả Owner yêu cầu và giữ hành vi không liên quan ổn định.','Các verification phù hợp phải PASS trước khi candidate hoàn tất.'],boundaries:[`Chỉ thay đổi project ${project}; không push/deploy từ Owner Console.`],non_goals:['Không mở rộng kiến trúc ngoài yêu cầu task nếu không cần.'],verification_profile:verification,priority,local_file_write:!!p.policy?.local_file_write,local_git_write:!!p.policy?.local_git_write,network_allowed:false});setTitle('');setDescription('');setAcceptance('');await onCreated()}catch(err:any){setError(err.message)}finally{setBusy(false)}}return <aside className="panel create-panel"><div className="panel-heading"><div className="panel-title"><Plus size={20}/><h2>Tạo task mới</h2></div></div><p className="panel-subtitle">Tạo tác vụ với workspace và MAR scheduler.</p><form onSubmit={submit}><label>Workspace<select value={project} onChange={e=>setProject(e.target.value)} required><option value="">Chọn workspace</option>{projects.filter((x:any)=>x.supported).map((x:any)=><option key={x.id} value={x.id}>{x.id}</option>)}</select></label><label>Worker<select disabled><option>Tự động · MAR scheduler</option></select></label><label>Tiêu đề task<input value={title} onChange={e=>setTitle(e.target.value)} required placeholder="Nhập tiêu đề ngắn gọn…"/></label><label>Mô tả<textarea value={description} onChange={e=>setDescription(e.target.value)} placeholder="Mô tả yêu cầu hoặc kết quả mong muốn…"/></label><details><summary><Settings size={15}/>Tùy chọn nâng cao</summary><div className="advanced-fields"><label>Verification<select value={verification} onChange={e=>setVerification(e.target.value)}><option value="go-standard">go-standard</option><option value="go-docs">go-docs</option></select></label><label>Priority<select value={priority} onChange={e=>setPriority(e.target.value)}><option>P0</option><option>P1</option><option>P2</option><option>P3</option></select></label><label>Acceptance<textarea value={acceptance} onChange={e=>setAcceptance(e.target.value)} placeholder="Mỗi dòng một tiêu chí…"/></label></div></details>{error&&<div className="form-error">{error}</div>}<div className="form-actions"><button type="button" onClick={()=>{setTitle('');setDescription('');setAcceptance('')}}>Hủy</button><button className="primary-button" disabled={busy||!p?.head||!title.trim()}>{busy?<LoaderCircle className="spin" size={16}/>:<Plus size={16}/>}Tạo task</button></div></form></aside>}

function FolderPickerModal({open,onClose,onPick}:any){const [data,setData]=useState<any>({path:'',roots:[],directories:[]}),[path,setPath]=useState(''),[busy,setBusy]=useState(false),[error,setError]=useState('');async function load(next=''){setBusy(true);setError('');try{const r:any=await browseProjectFolders(next);setData(r);setPath(r.path||next||'')}catch(e:any){setError(e?.message||'Không thể đọc thư mục.')}finally{setBusy(false)}}useEffect(()=>{if(open)load('')},[open]);if(!open)return null;return <div className="modal-backdrop" role="presentation" onMouseDown={e=>{if(e.target===e.currentTarget)onClose()}}><section className="folder-browser-modal" role="dialog" aria-modal="true" aria-label="Chọn workspace Git repository"><div className="panel-heading"><div className="panel-title"><FolderGit2 size={20}/><h2>Chọn thư mục workspace</h2></div><IconButton icon={XCircle} label="Đóng" onClick={onClose}/></div><div className="folder-path-row"><input value={path} onChange={e=>setPath(e.target.value)} placeholder="D:\\Projects\\MyApp" onKeyDown={e=>{if(e.key==='Enter'){e.preventDefault();load(path)}}}/><button disabled={busy||!path.trim()} onClick={()=>load(path)}>{busy?<LoaderCircle className="spin" size={16}/>:<FolderGit2 size={16}/>}Mở</button></div>{error&&<div className="form-error">{error}</div>}<div className="folder-browser-list">{!data.path&&(data.roots||[]).map((root:string)=><button className="folder-row" key={root} onClick={()=>load(root)}><FolderGit2 size={17}/><span>{root}</span></button>)}{data.path&&data.parent&&<button className="folder-row parent" onClick={()=>load(data.parent)}><span>↰</span><strong>Lên một cấp</strong><small>{data.parent}</small></button>}{(data.directories||[]).map((dir:any)=><button className="folder-row" key={dir.path} onClick={()=>load(dir.path)}><FolderGit2 size={17}/><span>{dir.name}</span></button>)}{data.path&&!(data.directories||[]).length&&!busy&&<div className="folder-empty">Không có thư mục con.</div>}</div><div className="folder-browser-footer"><div><span>Đang chọn</span><code>{data.path||'Chọn một ổ đĩa hoặc nhập đường dẫn ở trên'}</code></div><div><button onClick={onClose}>Hủy</button><button className="primary-button" disabled={!data.path||(!data.git_root&&(data.directories||[]).filter((dir:any)=>dir.git_root).length!==1)} onClick={()=>{const repoChildren=(data.directories||[]).filter((dir:any)=>dir.git_root);onPick(data.git_root?data.path:repoChildren[0].path);onClose()}}>{data.git_root?'Chọn repository này':'Dùng repository này'}</button></div></div></section></div>}

function WorkspacesPage({projects,reload,workspace,setWorkspace}:any){const [root,setRoot]=useState(''),[id,setId]=useState(''),[busy,setBusy]=useState(false),[pickerOpen,setPickerOpen]=useState(false),[error,setError]=useState('');async function register(){if(!root)return;setBusy(true);setError('');try{const created:any=await addProject(root,id);setRoot('');setId('');await reload();if(created?.project?.id)setWorkspace(created.project.id)}catch(e:any){setError(e?.message||'Không thể thêm workspace.')}finally{setBusy(false)}}return <><div className="page-header"><div><h1>Workspaces</h1><p>Đăng ký Git repository local và quyền mặc định.</p></div>{workspace&&<button className="scope-button" onClick={()=>setWorkspace('')}><XCircle size={15}/>Bỏ lọc: {workspace}</button>}</div><div className="workspace-grid"><section className="panel add-workspace"><div className="panel-heading"><div className="panel-title"><Plus size={20}/><h2>Thêm workspace</h2></div></div><label>Thư mục Git repository<div className="picker"><button onClick={()=>setPickerOpen(true)}><FolderGit2 size={16}/>Chọn thư mục</button><input value={root} onChange={e=>setRoot(e.target.value)} placeholder="D:\\Projects\\MyApp"/></div></label><small className="field-help">Có thể chọn bằng trình duyệt thư mục của MAR hoặc nhập/dán đường dẫn trực tiếp.</small><label>Project ID <span className="muted">(tùy chọn)</span><input value={id} onChange={e=>setId(e.target.value)} placeholder="Tự tạo nếu để trống"/></label>{error&&<div className="form-error">{error}</div>}<button className="primary-button" disabled={!root||busy} onClick={register}>{busy?<LoaderCircle className="spin" size={16}/>:<Plus size={16}/>}Thêm workspace</button></section><section className="panel workspace-list-panel"><div className="panel-heading"><div className="panel-title"><FolderGit2 size={20}/><h2>Đã đăng ký</h2></div><IconButton icon={RefreshCw} label="Làm mới" onClick={reload}/></div><div className="workspace-cards">{projects.map((p:any)=><WorkspaceCard key={p.id} p={p} reload={reload} selected={workspace===p.id} onSelect={()=>setWorkspace(p.id)}/>)}</div></section></div><FolderPickerModal open={pickerOpen} onClose={()=>setPickerOpen(false)} onPick={(path:string)=>{setRoot(path);setError('')}}/></>}
function WorkspaceCard({p,reload,selected,onSelect}:any){const [file,setFile]=useState(!!p.policy?.local_file_write),[git,setGit]=useState(!!p.policy?.local_git_write),[network,setNetwork]=useState(!!p.policy?.network_allowed),[push,setPush]=useState(!!p.policy?.remote_git_write),[deploy,setDeploy]=useState(!!p.policy?.deploy_allowed);useEffect(()=>{setFile(!!p.policy?.local_file_write);setGit(!!p.policy?.local_git_write);setNetwork(!!p.policy?.network_allowed);setPush(!!p.policy?.remote_git_write);setDeploy(!!p.policy?.deploy_allowed)},[p]);return <div className={`workspace-card ${selected?'selected':''}`}><div><div className="workspace-title"><strong>{p.id}</strong><span className={`support-chip ${p.supported?'ok':'bad'}`}>{p.supported?'Sẵn sàng':'Chưa hỗ trợ'}</span>{selected&&<span className="support-chip selected-chip">Đang chọn</span>}</div><code>{p.root}</code><small>{p.head?`HEAD ${String(p.head).slice(0,12)}`:(p.support_reason||p.head_error||'HEAD unavailable')}</small></div><div className="policy-checks"><label><input type="checkbox" checked={file} onChange={e=>setFile(e.target.checked)}/>Sửa file local</label><label><input type="checkbox" checked={git} onChange={e=>setGit(e.target.checked)}/>Local Git</label><label><input type="checkbox" checked={network} onChange={e=>setNetwork(e.target.checked)}/>Network: {network?'ON':'OFF'}</label><label><input type="checkbox" checked={push} onChange={e=>setPush(e.target.checked)}/>Push: {push?'ON':'OFF'}</label><label><input type="checkbox" checked={deploy} onChange={e=>setDeploy(e.target.checked)}/>Deploy: {deploy?'ON':'OFF'}</label><button className={`workspace-select-button ${selected?'selected':''}`} onClick={onSelect} disabled={selected}>{selected?'Đang chọn':'Chọn'}</button><button onClick={async()=>{await updateProjectPolicy(p.id,file,git,network,push,deploy);await reload()}}>Lưu quyền</button></div></div>}

function ConnectionsPage({runtime,reload}:any){const conns=runtime?.connections||[];return <><div className="page-header"><div><h1>Connections</h1><p>Provider configuration, transport readiness và live activity.</p></div></div><div className="connection-grid">{conns.map((c:any)=><ConnectionCard key={c.id} c={c} reload={reload}/>)}</div><section className="panel sandbox-card"><div className="sandbox-icon"><ShieldCheck size={24}/></div><div><h2>Sandbox bảo vệ Windows</h2><p>{runtime?.sandbox_detail||'Không có chi tiết.'}</p></div><StatusBadge state={runtime?.sandbox_host_ready?'COMPLETE':'BLOCKED'}/>{!runtime?.sandbox_host_ready&&<button className="primary-button" onClick={async()=>{await sandboxPrepare();await reload()}}>Chuẩn bị sandbox</button>}</section></>}
function ConnectionCard({c,reload}:any){const provider=connectionProvider(c),ready=routeReady(c),usable=connectionUsable(c),stage=connectionStageText(c),isTunnel=c.id==='openai-tunnel';const [open,setOpen]=useState(false),[busy,setBusy]=useState(''),[message,setMessage]=useState(''),[error,setError]=useState('');const url=String(c.stable_url||c.connection_url||c.temporary_url||'').trim();const urlKind=c.stable_url?'Stable URL':(c.temporary_url?'Temporary capability URL':'Connection URL');async function act(action:string){let path='';if(isTunnel)path=`/api/connections/openai-tunnel/${action}`;else if(action==='start')path='/api/connections/web-bridge/start';else if(action==='restart')path='/api/connections/web-bridge/restart';else if(action==='stop')path='/api/connections/web-bridge/stop';else path=`/api/connections/${encodeURIComponent(c.id)}/diagnose`;setBusy(action);setMessage('');setError('');try{await connectionAction(path);setMessage(action==='diagnose'?'Chẩn đoán hoàn tất.':action==='restart'?'Đã khởi động lại kết nối.':action==='stop'?'Đã tạm dừng kết nối.':'Đã yêu cầu kết nối.');await reload()}catch(err:any){setError(err?.message||'Thao tác kết nối thất bại.')}finally{setBusy('')}}async function copyLink(){if(!url)return;setMessage('');setError('');try{await navigator.clipboard.writeText(url);setMessage('Đã sao chép link kết nối.')}catch(err:any){setError(err?.message||'Không sao chép được link.')}}return <section className={`panel connection-card ${provider.toLowerCase()}`}><div className="connection-head"><div className={`provider-brand ${provider==='Claude'?'claude':''}`}><Bot size={24}/></div><div><h2>{c.name||provider}</h2><p>{c.transport||c.summary||''}</p></div><span className={`connection-chip ${usable?'connected':'idle'}`}><span className="live-dot"/>{usable?'Có thể dùng':stage}</span></div><div className="connection-state"><Metric label="Route" value={ready?'Ready':'Not ready'}/><Metric label="Client" value={isTunnel?(c.connected?'Attached':'Not attached'):(c.client_attached?'Attached':'Not attached')}/><Metric label="Tools" value={isTunnel?(c.connected?'Available':'Unknown'):(c.tools_discovered?'Discovered':'Not discovered')}/><Metric label="Usable" value={usable?'Yes':'No'}/></div>{c.identifier&&<div className="copy-row"><code>{c.identifier}</code><IconButton icon={Copy} label="Sao chép tunnel ID" onClick={()=>navigator.clipboard.writeText(c.identifier)}/></div>}{url&&<div className="connection-link"><span>{urlKind}</span><div className="copy-row"><code title={url}>{url}</code><button className="primary-button" onClick={copyLink}><Copy size={16}/>Sao chép link</button></div></div>}<p className="connection-summary">{c.summary||c.next_action||'Không có ghi chú.'}</p>{!url&&!isTunnel&&<div className="connection-hint">Chưa có capability URL. Hãy khởi động bridge để MAR tạo link kết nối.</div>}<div className="connection-actions">{!ready&&<button className="primary-button" disabled={!!busy} onClick={()=>act('start')}>{busy==='start'?<LoaderCircle className="spin" size={16}/>:<Play size={16}/>}Kết nối</button>}{c.running&&<button disabled={!!busy} onClick={()=>act('restart')}>{busy==='restart'?<LoaderCircle className="spin" size={16}/>:<RotateCcw size={16}/>}Khởi động lại</button>}{c.running&&<button disabled={!!busy} onClick={()=>act('stop')}><StopCircle size={16}/>Tạm dừng</button>}<button disabled={!!busy} onClick={()=>act('diagnose')}>{busy==='diagnose'?<LoaderCircle className="spin" size={16}/>:<TerminalSquare size={16}/>}Chẩn đoán</button><button onClick={()=>setOpen(!open)}><Settings size={16}/>{open?'Ẩn chi tiết':'Chi tiết'}</button></div>{message&&<div className="action-feedback success"><CheckCircle2 size={15}/>{message}</div>}{error&&<div className="action-feedback error"><AlertTriangle size={15}/>{error}</div>}{open&&<pre className="connection-raw">{JSON.stringify(c,null,2)}</pre>}</section>}

function UsagePage({usage,workspace}:any){const days=usage?.daily||[];const max=Math.max(1,...days.map((d:any)=>Number(d.total_tokens||0)));return <><div className="page-header"><div><h1>Usage</h1><p>{workspace?`Workspace: ${workspace}`:'Tất cả workspaces'} · durable result token counters.</p></div></div><section className="summary-cards three"><div className="summary-card"><div className="summary-icon blue"><Zap size={25}/></div><div><span>Hôm nay</span><strong>{usageWindowValue(usage?.today)}</strong><small>{fmtNumber(usage?.today?.input_tokens)} input · {fmtNumber(usage?.today?.output_tokens)} output</small></div></div><div className="summary-card"><div className="summary-icon cyan"><BarChart3 size={25}/></div><div><span>Tuần này</span><strong>{usageWindowValue(usage?.week)}</strong><small>Tuần lịch local</small></div></div><div className="summary-card"><div className="summary-icon green"><Database size={25}/></div><div><span>All time</span><strong>{usageWindowValue(usage?.all_time)}</strong><small>{fmtNumber(usage?.all_time?.results_with_token_data)} results measured</small></div></div></section><section className="panel"><div className="panel-heading"><div className="panel-title"><BarChart3 size={20}/><h2>30 ngày gần nhất</h2></div></div><div className="usage-bars">{days.map((d:any)=><div className="usage-bar-wrap" key={d.date} title={`${d.date}: ${fmtNumber(d.total_tokens)} tokens`}><div className="usage-bar" style={{height:`${Math.max(2,Number(d.total_tokens||0)/max*100)}%`}}/><span>{String(d.date||'').slice(8)}</span></div>)}</div></section><section className="panel"><div className="panel-heading"><div className="panel-title"><Database size={20}/><h2>Input / Output theo ngày</h2></div></div><div className="table-wrap"><table><thead><tr><th>Ngày</th><th>Input</th><th>Output</th><th>Tổng</th><th>Coverage</th></tr></thead><tbody>{[...days].reverse().map((d:any)=><tr key={d.date}><td>{d.date}</td><td>{fmtNumber(d.input_tokens)}</td><td>{fmtNumber(d.output_tokens)}</td><td>{fmtNumber(d.total_tokens)}</td><td>{fmtNumber(d.results_with_token_data)}/{fmtNumber(d.results)}</td></tr>)}</tbody></table></div></section></>}

function DiagnosticsPage({runtime,reload}:any){return <><div className="page-header"><div><h1>Diagnostics</h1><p>Technical evidence dành cho Tech Lead/debug.</p></div><button onClick={reload}><RefreshCw size={16}/>Làm mới</button></div><div className="diagnostic-grid"><section className="panel"><div className="panel-heading"><div className="panel-title"><TerminalSquare size={20}/><h2>Runtime</h2></div></div><pre>{JSON.stringify(runtime,null,2)}</pre></section><section className="panel"><div className="panel-heading"><div className="panel-title"><ShieldCheck size={20}/><h2>Normal flow</h2></div></div><div className="flow-principle"><span>Owner</span><span>→</span><span>Tech Lead Web</span><span>→</span><span>MAR</span><span>→</span><span>Worker</span><span>→</span><span>Verified result</span></div><button className="primary-button" disabled={runtime?.sandbox_host_ready} onClick={async()=>{await sandboxPrepare();await reload()}}><ShieldCheck size={16}/>{runtime?.sandbox_host_ready?'Sandbox ready':'Chuẩn bị sandbox'}</button></section></div></>}

export default function App(){
  const initialHash=(window.location.hash.replace('#/','')||'live') as View
  const [view,setViewState]=useState<View>(views.some(v=>v.id===initialHash)?initialHash:'live')
  const [runtime,setRuntime]=useState<any>(null),[projects,setProjects]=useState<any[]>([]),[tasks,setTasks]=useState<any[]>([]),[usage,setUsage]=useState<any>(null),[workspace,setWorkspaceState]=useState(()=>localStorage.getItem('mar.owner.workspace.react.v1')||''),[tokenSamples,setTokenSamples]=useState<TokenSample[]>([]),[error,setError]=useState('')
  const workspaceRef=useRef(workspace);workspaceRef.current=workspace
  const setView=(next:View)=>{setViewState(next);history.replaceState(null,'',`#/${next}`)}
  const setWorkspace=(value:string)=>{setWorkspaceState(value);localStorage.setItem('mar.owner.workspace.react.v1',value)}
  const reloadRuntime=async()=>{try{setRuntime(await getRuntime());setError('')}catch(e:any){setError(e.message)}}
  const reloadProjects=async()=>{const r:any=await getProjects();setProjects(r.projects||[])}
  const reloadTasks=async()=>{const r:any=await getTasks();setTasks(r.tasks||[])}
  const reloadUsage=async()=>{setUsage(await getUsage(workspaceRef.current))}
  useEffect(()=>{Promise.all([reloadRuntime(),reloadProjects(),reloadTasks(),reloadUsage()]);const timer=setInterval(async()=>{await Promise.allSettled([reloadRuntime(),reloadTasks()])},2000);const usageTimer=setInterval(reloadUsage,30000);const onHashChange=()=>{const next=(window.location.hash.replace('#/','')||'live') as View;if(views.some(v=>v.id===next))setViewState(next)};window.addEventListener('hashchange',onHashChange);return()=>{clearInterval(timer);clearInterval(usageTimer);window.removeEventListener('hashchange',onHashChange)}},[])
  useEffect(()=>{if(workspace&&projects.length&&!projects.some((p:any)=>p.id===workspace))setWorkspace('')},[projects,workspace])
  useEffect(()=>{reloadUsage()},[workspace])
  const scopedTasks=useMemo(()=>workspace?tasks.filter((t:any)=>t.project_id===workspace):tasks,[tasks,workspace])
  const aggregate=useMemo(()=>liveAggregate(scopedTasks),[scopedTasks])
  useEffect(()=>{if(!aggregate.tokenTasks)return;setTokenSamples(prev=>[...prev,{at:Date.now(),input:aggregate.input,output:aggregate.output}].slice(-40))},[aggregate.input,aggregate.output,aggregate.tokenTasks])
  let content:React.ReactNode
  if(view==='live')content=<LiveOperations runtime={runtime} tasks={tasks} usage={usage} tokenSamples={tokenSamples} setView={setView} workspace={workspace}/>
  else if(view==='overview')content=<Overview runtime={runtime} tasks={tasks} usage={usage} setView={setView} workspace={workspace} projects={projects}/>
  else if(view==='tasks')content=<TasksPage tasks={tasks} projects={projects} workspace={workspace} reloadTasks={reloadTasks}/>
  else if(view==='workspaces')content=<WorkspacesPage projects={projects} reload={reloadProjects} workspace={workspace} setWorkspace={setWorkspace}/>
  else if(view==='connections')content=<ConnectionsPage runtime={runtime} reload={reloadRuntime}/>
  else if(view==='usage')content=<UsagePage usage={usage} workspace={workspace}/>
  else content=<DiagnosticsPage runtime={runtime} reload={reloadRuntime}/>
  return <Shell view={view} setView={setView} runtime={runtime} workspace={workspace} setWorkspace={setWorkspace} projects={projects}>{error&&<div className="global-error"><AlertTriangle size={16}/>{error}</div>}{content}</Shell>
}
