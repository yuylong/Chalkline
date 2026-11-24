// SPA 路由注册（web/src/app/routes.tsx）。
// 鉴权只判断「内存中是否有 token」，权限判定一律由后端响应决定。
// 注：react-router v7（7.18）的 BrowserRouter 等 DOM 组件在包根导出；
// "./dom" 子路径仅含 RSC 水合辅助，故统一从包根导入。
import { BrowserRouter, Navigate, Route, Routes } from 'react-router'
import type { ReactElement } from 'react'
import Layout from '../components/Layout'
import { getToken } from '../shared/api'
import AuditList from '../features/audit/List'
import Login from '../features/auth/Login'
import ProviderConfigs from '../features/integrations/Providers'
import CurrentIterationPage from '../features/iterations/Current'
import IterationDetail from '../features/iterations/Detail'
import ProductDetail from '../features/products/Detail'
import ProductList from '../features/products/List'
import ReviewQueue from '../features/reviews/Queue'
import TaskDetail from '../features/tasks/Detail'
import UserList from '../features/users/List'
import WebhookList from '../features/webhooks/List'
import WorkflowList from '../features/workflows/List'

function RequireAuth({ children }: { children: ReactElement }) {
  if (!getToken()) return <Navigate to="/login" replace />
  return children
}

export default function AppRoutes() {
  return (
    <BrowserRouter>
      <Routes>
        <Route path="/login" element={<Login />} />
        <Route
          element={
            <RequireAuth>
              <Layout />
            </RequireAuth>
          }
        >
          <Route path="/" element={<CurrentIterationPage />} />
          <Route path="/products" element={<ProductList />} />
          <Route path="/products/:id" element={<ProductDetail />} />
          <Route path="/iterations/:id" element={<IterationDetail />} />
          <Route path="/tasks/:id" element={<TaskDetail />} />
          <Route path="/reviews" element={<ReviewQueue />} />
          <Route path="/audit" element={<AuditList />} />
          <Route path="/webhooks" element={<WebhookList />} />
          <Route path="/users" element={<UserList />} />
          <Route path="/workflows" element={<WorkflowList />} />
          <Route path="/integrations" element={<ProviderConfigs />} />
        </Route>
        <Route path="*" element={<Navigate to="/" replace />} />
      </Routes>
    </BrowserRouter>
  )
}
