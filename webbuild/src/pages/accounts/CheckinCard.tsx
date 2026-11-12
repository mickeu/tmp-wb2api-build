import { Button, Card } from '@heroui/react'
import { useState } from 'react'
import { post } from '../../lib/api'
import { toastError, useStartedToast } from '../../lib/hooks'
import type { Account } from '../../lib/types'

/// 签到聚合卡：账号多的时候「全部签到」藏在右上动作菜单里看不见，
/// 这里把今日签到进度和总积分摆到台面上，一眼能看到状态并直接签到。
export function CheckinCard({ accounts }: { accounts: Account[] }) {
  const started = useStartedToast()
  const [busy, setBusy] = useState(false)
  const total = accounts.length
  const done = accounts.filter((a) => a.checkin_done).length
  const credits = accounts.reduce((sum, a) => sum + (a.credits ?? 0), 0)
  const allDone = total > 0 && done >= total

  const fire = async () => {
    setBusy(true)
    try {
      await post('/panel/api/checkin_all')
      started('已开始签到')
    } catch (e) {
      toastError(e)
    } finally {
      setBusy(false)
    }
  }

  if (total === 0) return null

  return (
    <Card className="[--radius:1rem]">
      <Card.Content className="flex flex-row items-center gap-3 py-3">
        <div className="flex size-10 shrink-0 items-center justify-center rounded-full bg-success/12 text-success">
          <svg viewBox="0 0 24 24" className="size-5" fill="none" stroke="currentColor" strokeWidth="2">
            <path d="M4 12.5l5 5L20 6.5" strokeLinecap="round" strokeLinejoin="round" />
          </svg>
        </div>
        <div className="min-w-0 flex-1">
          <div className="flex items-baseline gap-2">
            <span className="text-sm font-medium">每日签到</span>
            <span className="text-xs text-foreground/50">
              今日 {done}/{total}
            </span>
          </div>
          <div className="text-xs text-foreground/50">总积分 {credits.toLocaleString()}</div>
        </div>
        <Button
          size="sm"
          variant={allDone ? 'soft' : 'secondary'}
          isDisabled={busy}
          onPress={() => void fire()}
        >
          {busy ? '签到中' : allDone ? '再次签到' : '一键签到'}
        </Button>
      </Card.Content>
    </Card>
  )
}
