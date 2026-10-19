// Closing the page that runs an assistant's scene job interrupts it until
// the lease runs out and another page claims it. When this is the browser's
// only creative workspace tab nobody else will, so closing asks first;
// with other tabs open it does not, as one of them takes the job over.
// Tabs count each other over a BroadcastChannel; one that crashes without
// saying goodbye still counts, which only means not asking.

type Message = { type: 'hello' | 'here' | 'bye'; id: string }
type Target = Pick<Window, 'addEventListener' | 'removeEventListener'>
type Channel = { postMessage(m: Message): void; close(): void; onmessage: ((e: MessageEvent<Message>) => void) | null }

export const channelName = 'creative-workspace'

export class LeaveGuard {
  private readonly id = crypto.randomUUID()
  private readonly peers = new Set<string>()
  private readonly target: Target
  private readonly open: (name: string) => Channel
  private channel: Channel | null = null
  private holding = false
  private asking = false
  private readonly ask = (e: BeforeUnloadEvent) => {
    e.preventDefault()
    e.returnValue = ''
  }
  private readonly hide = () => this.post('bye')
  private readonly show = (e: PageTransitionEvent) => {
    if (e.persisted) this.post('hello')
  }

  constructor(target: Target = window, open: (name: string) => Channel = (name) => new BroadcastChannel(name)) {
    this.target = target
    this.open = open
  }

  start(): () => void {
    try {
      this.channel = this.open(channelName)
      this.channel.onmessage = (e) => this.receive(e.data)
    } catch {
      this.channel = null
    }
    this.post('hello')
    this.target.addEventListener('pagehide', this.hide)
    this.target.addEventListener('pageshow', this.show as EventListener)
    return () => {
      this.post('bye')
      this.target.removeEventListener('pagehide', this.hide)
      this.target.removeEventListener('pageshow', this.show as EventListener)
      this.channel?.close()
      this.channel = null
      this.peers.clear()
      this.setHolding(false)
    }
  }

  /** No other creative workspace tab is open in this browser. */
  get alone(): boolean {
    return this.peers.size === 0
  }

  setHolding(holding: boolean) {
    this.holding = holding
    this.update()
  }

  private post(type: Message['type']) {
    try {
      this.channel?.postMessage({ type, id: this.id })
    } catch {
      // A closed channel: nothing to tell.
    }
  }

  private receive(m: Message | undefined) {
    if (!m || typeof m.id !== 'string' || m.id === this.id) return
    if (m.type === 'bye') this.peers.delete(m.id)
    else {
      this.peers.add(m.id)
      if (m.type === 'hello') this.post('here')
    }
    this.update()
  }

  private update() {
    const ask = this.holding && this.alone
    if (ask === this.asking) return
    this.asking = ask
    if (ask) this.target.addEventListener('beforeunload', this.ask)
    else this.target.removeEventListener('beforeunload', this.ask)
  }
}
