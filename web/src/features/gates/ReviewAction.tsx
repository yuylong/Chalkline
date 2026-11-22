// 门禁审批操作：通过 / 驳回（驳回必填理由）。评审队列与工作项详情共用。
// 权限与门禁判定完全在服务端；前端只发起请求并按响应呈现。
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { App, Button, Form, Input, Modal, Popconfirm, Space } from 'antd'
import { useState } from 'react'
import { apiPost } from '../../shared/api'
import type { NodeOpResult } from '../../shared/types'

interface ReviewActionsProps {
  scope: 'task' | 'iteration'
  workItemId: string
  nodeId: string
  /** 操作成功后追加失效的缓存键（页面自行传入）。 */
  onDone?: () => void
}

export function ReviewActions({ scope, workItemId, nodeId, onDone }: ReviewActionsProps) {
  const queryClient = useQueryClient()
  const { message } = App.useApp()
  const [rejectOpen, setRejectOpen] = useState(false)
  const [form] = Form.useForm<{ comment: string }>()

  const invalidate = () => {
    const itemKey = scope === 'task' ? 'task' : 'iteration'
    const wfKey = scope === 'task' ? 'task-workflow' : 'iteration-workflow'
    queryClient.invalidateQueries({ queryKey: [itemKey, workItemId] })
    queryClient.invalidateQueries({ queryKey: [wfKey, workItemId] })
    queryClient.invalidateQueries({ queryKey: ['next-action', scope, workItemId] })
    queryClient.invalidateQueries({ queryKey: ['reviews'] })
    queryClient.invalidateQueries({ queryKey: ['tasks'] })
    onDone?.()
  }

  const reviewMut = useMutation({
    mutationFn: (v: { decision: 'approve' | 'reject'; comment: string }) =>
      apiPost<NodeOpResult>(`/${scope}s/${workItemId}/steps/review`, {
        nodeId,
        decision: v.decision,
        comment: v.comment,
        expectedVersion: 0,
      }),
    onSuccess: (res) => {
      if (res.gate?.passed) {
        message.success('门禁已通过')
      } else if (res.node.status === 'rejected') {
        message.warning('已驳回，等待提交人修改后重提')
      } else {
        message.info(`评审已记录（${res.gate ? `${res.gate.approvals}/${res.gate.approvalsNeeded}` : '等待会签'}）`)
      }
      setRejectOpen(false)
      form.resetFields()
      invalidate()
    },
    onError: (err: Error) => message.error(err.message),
  })

  return (
    <Space>
      <Popconfirm
        title="确认通过该门禁？"
        onConfirm={() => reviewMut.mutate({ decision: 'approve', comment: '' })}
      >
        <Button type="primary" size="small" loading={reviewMut.isPending && !rejectOpen}>
          通过
        </Button>
      </Popconfirm>
      <Button danger size="small" onClick={() => setRejectOpen(true)}>
        驳回
      </Button>
      <Modal
        title="驳回门禁"
        open={rejectOpen}
        confirmLoading={reviewMut.isPending}
        onCancel={() => setRejectOpen(false)}
        onOk={() => form.submit()}
        destroyOnHidden
      >
        <Form
          form={form}
          layout="vertical"
          onFinish={(v: { comment: string }) => reviewMut.mutate({ decision: 'reject', comment: v.comment })}
          style={{ marginTop: 16 }}
        >
          <Form.Item
            name="comment"
            label="驳回理由（必填）"
            rules={[{ required: true, message: '驳回必须填写理由' }]}
          >
            <Input.TextArea rows={3} />
          </Form.Item>
        </Form>
      </Modal>
    </Space>
  )
}
