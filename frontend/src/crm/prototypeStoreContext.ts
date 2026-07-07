import { createContext, useContext } from 'react'
import type {
  Channel,
  Customer,
  Order,
  Package,
  Platform,
  PricingMode,
  Reminder,
  ShootType,
  Slot,
  SlotType,
} from './prototypeData'

export interface AddCustomerInput {
  displayName: string
  platform: Platform
  handle: string
  channel: Channel
}

export interface PackageInput {
  id?: string
  name: string
  shootType: ShootType
  pricingMode: PricingMode
  basePriceYuan: number
  durationMinutes: number
  shotCountMin: number
  shotCountMax: number
  rawDeliveryCount: number | null
  retouchCount: number
  note: string
}

export interface SlotInput {
  id?: string
  type: SlotType
  date: string
  start: string
  end: string
  note: string
  orderId: string | null
}

export interface PrototypeState {
  customers: Customer[]
  packages: Package[]
  orders: Order[]
  slots: Slot[]
  reminders: Reminder[]
}

export interface PrototypeStore extends PrototypeState {
  addCustomer(input: AddCustomerInput): string
  addNote(customerId: string, content: string): void
  markReminderDone(reminderId: string): void
  dismissReminder(reminderId: string): void
  settleOrder(orderId: string): void
  upsertPackage(input: PackageInput): void
  setPackageStatus(packageId: string, status: Package['status']): void
  upsertSlot(input: SlotInput): void
  deleteSlot(slotId: string): void
}

export const PrototypeContext = createContext<PrototypeStore | null>(null)

export function usePrototypeStore(): PrototypeStore {
  const context = useContext(PrototypeContext)
  if (!context) throw new Error('usePrototypeStore must be used inside PrototypeProvider')
  return context
}
