// 迭代进度可视化：燃尽（按任务完成数近似）、状态分布、成员负载。
// 完成时间取各任务工作流终态节点的 completedAt；无实例信息的按当日计。
import dayjs from 'dayjs'
import {
  Bar,
  BarChart,
  CartesianGrid,
  Cell,
  Legend,
  Line,
  LineChart,
  Pie,
  PieChart,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from 'recharts'
import type { InstanceView, MemberView, TaskView } from '../../shared/types'
import { userLabel } from '../../shared/status'

interface ChartsProps {
  tasks: TaskView[]
  workflows: Map<string, InstanceView | undefined>
  members: MemberView[]
  startsOn: string
  endsOn: string
}

const STATUS_COLORS: Record<string, string> = {
  open: '#1677ff',
  done: '#52c41a',
  cancelled: '#bfbfbf',
}

const STATUS_LABELS: Record<string, string> = {
  open: '未终结',
  done: '已完成',
  cancelled: '已取消',
}

function maxCompletedAt(wf: InstanceView): string | undefined {
  let max: string | undefined
  for (const n of wf.nodes) {
    if (n.completedAt && (!max || n.completedAt > max)) max = n.completedAt
  }
  return max
}

interface BurndownPoint {
  date: string
  remaining: number
  ideal: number
}

function buildBurndown(
  tasks: TaskView[],
  workflows: Map<string, InstanceView | undefined>,
  startsOn: string,
  endsOn: string,
): BurndownPoint[] {
  const total = tasks.length
  const start = dayjs(startsOn)
  if (total === 0 || !start.isValid()) return []
  const end = dayjs(endsOn)
  const today = dayjs().startOf('day')
  const horizon = end.isValid() && end.isAfter(today) ? end : today
  if (horizon.isBefore(start)) return []
  const duration = Math.max(1, end.isValid() ? end.diff(start, 'day') : 14)
  const days = Math.min(horizon.diff(start, 'day') + 1, 365)

  const doneAt: dayjs.Dayjs[] = []
  for (const t of tasks) {
    if (t.status !== 'done') continue
    const wf = workflows.get(t.id)
    const ts = wf ? maxCompletedAt(wf) : undefined
    doneAt.push(ts && dayjs(ts).isValid() ? dayjs(ts) : today)
  }

  const points: BurndownPoint[] = []
  for (let i = 0; i < days; i++) {
    const dayEnd = start.add(i, 'day').endOf('day')
    const done = doneAt.filter((d) => !d.isAfter(dayEnd)).length
    points.push({
      date: start.add(i, 'day').format('MM-DD'),
      remaining: total - done,
      ideal: Math.max(0, Math.round(total * (1 - i / duration))),
    })
  }
  return points
}

export default function IterationCharts({ tasks, workflows, members, startsOn, endsOn }: ChartsProps) {
  const statusData = ['open', 'done', 'cancelled']
    .map((s) => ({ name: STATUS_LABELS[s], value: tasks.filter((t) => t.status === s).length, key: s }))
    .filter((d) => d.value > 0)

  const nameOf = (uid: string) => {
    const m = members.find((x) => x.userId === uid)
    return m ? userLabel(m) : '未知用户'
  }
  const loadMap = new Map<string, number>()
  for (const t of tasks) {
    if (t.status !== 'open') continue
    const key = t.assigneeId ?? ''
    loadMap.set(key, (loadMap.get(key) ?? 0) + 1)
  }
  const loadData = [...loadMap.entries()]
    .map(([uid, count]) => ({ name: uid ? nameOf(uid) : '未指派', count }))
    .sort((a, b) => b.count - a.count)

  const burndown = buildBurndown(tasks, workflows, startsOn, endsOn)

  return (
    <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(320px, 1fr))', gap: 16 }}>
      <div style={{ background: '#fff', padding: 12, border: '1px solid #f0f0f0', borderRadius: 8 }}>
        <h4 style={{ margin: '4px 0 12px' }}>燃尽图（剩余任务数 · 理想线）</h4>
        {burndown.length === 0 ? (
          <div style={{ color: '#999', padding: 24 }}>暂无可绘制的迭代数据</div>
        ) : (
          <ResponsiveContainer width="100%" height={240}>
            <LineChart data={burndown} margin={{ top: 4, right: 12, bottom: 0, left: -16 }}>
              <CartesianGrid strokeDasharray="3 3" />
              <XAxis dataKey="date" fontSize={11} />
              <YAxis allowDecimals={false} fontSize={11} />
              <Tooltip />
              <Legend />
              <Line type="monotone" dataKey="remaining" name="剩余" stroke="#1677ff" dot={false} />
              <Line
                type="monotone"
                dataKey="ideal"
                name="理想"
                stroke="#d46b08"
                strokeDasharray="6 4"
                dot={false}
              />
            </LineChart>
          </ResponsiveContainer>
        )}
      </div>

      <div style={{ background: '#fff', padding: 12, border: '1px solid #f0f0f0', borderRadius: 8 }}>
        <h4 style={{ margin: '4px 0 12px' }}>任务状态分布</h4>
        {statusData.length === 0 ? (
          <div style={{ color: '#999', padding: 24 }}>暂无任务</div>
        ) : (
          <ResponsiveContainer width="100%" height={240}>
            <PieChart>
              <Pie
                data={statusData}
                dataKey="value"
                nameKey="name"
                innerRadius={48}
                outerRadius={80}
                label
              >
                {statusData.map((d) => (
                  <Cell key={d.key} fill={STATUS_COLORS[d.key]} />
                ))}
              </Pie>
              <Tooltip />
              <Legend />
            </PieChart>
          </ResponsiveContainer>
        )}
      </div>

      <div style={{ background: '#fff', padding: 12, border: '1px solid #f0f0f0', borderRadius: 8 }}>
        <h4 style={{ margin: '4px 0 12px' }}>成员负载（未终结任务数）</h4>
        {loadData.length === 0 ? (
          <div style={{ color: '#999', padding: 24 }}>暂无未终结任务</div>
        ) : (
          <ResponsiveContainer width="100%" height={240}>
            <BarChart data={loadData} margin={{ top: 4, right: 12, bottom: 0, left: -16 }}>
              <CartesianGrid strokeDasharray="3 3" />
              <XAxis dataKey="name" fontSize={11} />
              <YAxis allowDecimals={false} fontSize={11} />
              <Tooltip />
              <Bar dataKey="count" name="未终结任务" fill="#1677ff" radius={[4, 4, 0, 0]} />
            </BarChart>
          </ResponsiveContainer>
        )}
      </div>
    </div>
  )
}
