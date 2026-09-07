import { useSearchParams } from 'react-router-dom'
import OrderWorkspace from '../components/orders/OrderWorkspace'

export default function OrdersPage() {
  const [params] = useSearchParams()
  return (
    <>
      <header className="topbar">
        <div>
          <h1>订单</h1>
          <div className="sub">按状态、客户与尾款收取情况管理约单</div>
        </div>
      </header>
      <main className="content">
        <OrderWorkspace focusOrderId={params.get('order') ?? undefined} />
      </main>
    </>
  )
}
