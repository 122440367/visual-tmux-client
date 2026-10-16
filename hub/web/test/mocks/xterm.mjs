export class Terminal {
  constructor(options) {
    this.options = options
    this.unicode = { activeVersion: '11' }
    this.cols = 80
    this.rows = 24
  }

  loadAddon() {}
  open() {}
  onData() {}
  onBinary() {}
  attachCustomKeyEventHandler() {}
  onResize() {}
  write() {}
  getSelection() {
    return this.selected ?? ''
  }
  paste(text) {
    this.pasted = [...(this.pasted ?? []), text]
  }
  selectAll() {
    this.allSelected = true
  }
  clear() {
    this.cleared = (this.cleared ?? 0) + 1
  }
  focus() {}
  dispose() {}
}

export class FitAddon {
  constructor() {
    this.fitCount = 0
  }
  fit() {
    this.fitCount++
  }
  proposeDimensions() {
    return { cols: 80, rows: 24 }
  }
}

export class Unicode11Addon {}

export default {
  Terminal,
  FitAddon,
  Unicode11Addon,
}
