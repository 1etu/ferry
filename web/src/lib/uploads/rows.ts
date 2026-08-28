import { isInFlight, isStartable, type Row } from './row';

export class ProgressBatch {
  #rows: Row[] = [];
  #frame: number | undefined;

  add(row: Row, publish: (rows: readonly Row[]) => void): void {
    if (!this.#rows.includes(row)) this.#rows.push(row);
    if (this.#frame !== undefined) return;
    this.#frame = requestAnimationFrame(() => {
      this.#frame = undefined;
      const rows = this.#rows;
      this.#rows = [];
      publish(rows);
    });
  }

  forget(row: Row): void {
    this.#rows = this.#rows.filter((candidate) => candidate !== row);
  }
}

export class RowIndex {
  #rows: Row[] = [];
  #byTransferId = new Map<string, Row>();

  get all(): readonly Row[] {
    return this.#rows;
  }

  byId(id: string): Row | undefined {
    return this.#rows.find((row) => row.id === id);
  }

  byTransferId(transferId: string): Row | undefined {
    return this.#byTransferId.get(transferId);
  }

  push(row: Row): void {
    this.#rows.push(row);
    this.index(row);
  }

  index(row: Row): void {
    if (row.transferId !== undefined) this.#byTransferId.set(row.transferId, row);
  }

  forgetTransfer(row: Row): void {
    if (row.transferId !== undefined) this.#byTransferId.delete(row.transferId);
  }

  drop(row: Row): void {
    this.#rows = this.#rows.filter((candidate) => candidate !== row);
    this.forgetTransfer(row);
  }

  startable(now: number): Row[] {
    return this.#rows.filter((row) => isStartable(row, now));
  }

  activeCount(): number {
    return this.#rows.filter((row) => isInFlight(row.status)).length;
  }

  hasWork(): boolean {
    return this.#rows.some(
      (row) => isInFlight(row.status) || (row.status === 'queued' && row.retryAt > 0),
    );
  }

  deferQueued(retryAt: number): void {
    for (const row of this.#rows) if (row.status === 'queued') row.retryAt = retryAt;
  }

  releaseOffline(): void {
    for (const row of this.#rows) {
      if (isInFlight(row.status) && row.upload === undefined) row.retryAt = 0;
    }
  }
}
