import OrderWorkspace from '../components/orders/OrderWorkspace'

export default function OrdersPage() {
  return (
    <>
      <header className="topbar">
        <div>
          <h1>订单</h1>
          <div className="sub">按状态、客户与尾款收取情况管理约单</div>
        </div>
      </header>
      <main className="content">
        <OrderWorkspace />
      </main>
    </>
  )
}
