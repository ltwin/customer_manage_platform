import { useCallback, useMemo, useState } from 'react'
import {
  customers as initialCustomers,
  orders as initialOrders,
  packages as initialPackages,
  reminders as initialReminders,
  slots as initialSlots,
} from './prototypeData'
import type { Customer, Package, Slot } from './prototypeData'
import { PrototypeContext } from './prototypeStoreContext'
import type { AddCustomerInput, PackageInput, PrototypeState, PrototypeStore, SlotInput } from './prototypeStoreContext'

function clone<T>(value: T): T {
  return JSON.parse(JSON.stringify(value)) as T
}

function initialState(): PrototypeState {
  return {
    customers: clone(initialCustomers),
    packages: clone(initialPackages),
    orders: clone(initialOrders),
    slots: clone(initialSlots),
    reminders: clone(initialReminders),
  }
}

function nowStamp(): string {
  return `2026-07-06 ${new Date().toTimeString().slice(0, 5)}`
}

export function PrototypeProvider({ children }: { children: React.ReactNode }) {
  const [state, setState] = useState<PrototypeState>(() => initialState())

  const addCustomer = useCallback((input: AddCustomerInput) => {
    const id = `c_new_${Date.now()}`
    const avatar = ['a1', 'a2', 'a3', 'a4', 'a5'][state.customers.length % 5]
    const customer: Customer = {
      id,
      display_name: input.displayName,
      real_name: '',
      phone: '',
      birthday: '',
      channel: input.channel,
      referrer_customer_id: null,
      status: 'active',
      avatar,
      identities: [{ platform: input.platform, handle: input.handle, remark: '建档账号' }],
      notes: [],
      created_at: '2026-07-06',
    }
    setState((current) => ({ ...current, customers: [customer, ...current.customers] }))
    return id
  }, [state.customers.length])

  const addNote = useCallback((customerId: string, content: string) => {
    setState((current) => ({
      ...current,
      customers: current.customers.map((customer) =>
        customer.id === customerId
          ? { ...customer, notes: [{ time: nowStamp(), content }, ...customer.notes] }
          : customer,
      ),
    }))
  }, [])

  const markReminderDone = useCallback((reminderId: string) => {
    setState((current) => ({
      ...current,
      reminders: current.reminders.map((reminder) =>
        reminder.id === reminderId ? { ...reminder, status: 'done' } : reminder,
      ),
    }))
  }, [])

  const dismissReminder = useCallback((reminderId: string) => {
    setState((current) => ({
      ...current,
      reminders: current.reminders.map((reminder) =>
        reminder.id === reminderId ? { ...reminder, status: 'dismissed' } : reminder,
      ),
    }))
  }, [])

  const settleOrder = useCallback((orderId: string) => {
    setState((current) => ({
      ...current,
      orders: current.orders.map((order) =>
        order.id === orderId ? { ...order, balance_paid: true } : order,
      ),
    }))
  }, [])

  const upsertPackage = useCallback((input: PackageInput) => {
    const pkg: Package = {
      id: input.id ?? `p_new_${Date.now()}`,
      name: input.name,
      shoot_type: input.shootType,
      pricing_mode: input.pricingMode,
      base_price: Math.max(0, Math.round(input.basePriceYuan * 100)),
      duration_minutes: input.durationMinutes,
      shot_count_min: input.shotCountMin,
      shot_count_max: input.shotCountMax,
      raw_delivery_count: input.rawDeliveryCount,
      retouch_count: input.retouchCount,
      status: 'active',
      note: input.note,
    }
    setState((current) => ({
      ...current,
      packages: input.id
        ? current.packages.map((item) => (item.id === input.id ? { ...pkg, status: item.status } : item))
        : [pkg, ...current.packages],
    }))
  }, [])

  const setPackageStatus = useCallback((packageId: string, status: Package['status']) => {
    setState((current) => ({
      ...current,
      packages: current.packages.map((pkg) => (pkg.id === packageId ? { ...pkg, status } : pkg)),
    }))
  }, [])

  const upsertSlot = useCallback((input: SlotInput) => {
    const slot: Slot = {
      id: input.id ?? `s_new_${Date.now()}`,
      date: input.date,
      start: input.start,
      end: input.end,
      type: input.type,
      order_id: input.type === 'shoot' ? input.orderId : null,
      note: input.note,
    }
    setState((current) => ({
      ...current,
      slots: input.id ? current.slots.map((item) => (item.id === input.id ? slot : item)) : [...current.slots, slot],
    }))
  }, [])

  const deleteSlot = useCallback((slotId: string) => {
    setState((current) => ({ ...current, slots: current.slots.filter((slot) => slot.id !== slotId) }))
  }, [])

  const value = useMemo<PrototypeStore>(() => ({
    ...state,
    addCustomer,
    addNote,
    markReminderDone,
    dismissReminder,
    settleOrder,
    upsertPackage,
    setPackageStatus,
    upsertSlot,
    deleteSlot,
  }), [
    state,
    addCustomer,
    addNote,
    markReminderDone,
    dismissReminder,
    settleOrder,
    upsertPackage,
    setPackageStatus,
    upsertSlot,
    deleteSlot,
  ])

  return <PrototypeContext.Provider value={value}>{children}</PrototypeContext.Provider>
}
