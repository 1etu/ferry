import { SvelteMap } from 'svelte/reactivity';
import { type ErrorCode, type Transfer } from '$lib/api/client';
import { session } from '$lib/session.svelte';
import { restartAttempt, startAttempt } from './attempt';
import { classify } from './failure';
import { fingerprintFile } from './fingerprint';
import {
  awaitsFile,
  isInFlight,
  isSettled,
  pickedRow,
  publicItem,
  replaceFile,
  requeue,
  restoredRow,
  rowAwaiting,
  settle,
  settledByServer,
  storedItem,
  transferIdOf,
  type Row,
  type UploadItem,
} from './row';
import { ProgressBatch, RowIndex } from './rows';
import { ItemStore, readStoredItems } from './storage';
import { uploadFactory } from './transport';
import { newUploadNonce, type DetailedError } from './tus';
import { watchUploads } from './watchdog';

export { type UploadItem, type UploadStatus } from './row';
export { setUploadFactory, type UploadFactory } from './transport';

const maxParallelUploads = 4;
const sessionRetryMs = 5000;

function leaveForServerSweep(): undefined {
  return undefined;
}

export class UploadQueue {
  #rows = new RowIndex();
  #byId = new SvelteMap<string, UploadItem>();
  #progressed = new ProgressBatch();
  #stopWatching: (() => void) | undefined;
  #renewing: Promise<void> | undefined;
  #isRestored = false;
  #store = new ItemStore(() => this.#rows.all.flatMap((row) => storedItem(row) ?? []));

  readonly items = $derived([...this.#byId.values()].reverse());

  async add(files: Iterable<File>): Promise<void> {
    const rows = [...files].map((file) => this.#push(pickedRow(file)));
    await Promise.all(
      rows.map(async (row) => {
        await this.#fingerprint(row);
        this.#pump();
      }),
    );
    this.#store.persist();
  }

  async cancel(id: string): Promise<void> {
    const row = this.#rows.byId(id);
    if (row === undefined || isSettled(row.status)) return;
    const { upload, uploadUrl } = row;
    this.#settle(row, 'canceled');
    if (upload !== undefined) await upload.abort(true).catch(leaveForServerSweep);
    else if (uploadUrl !== undefined) await this.#terminate(uploadUrl);
  }

  async resume(id: string, file: File): Promise<void> {
    const tapped = this.#rows.byId(id);
    if (tapped === undefined || !awaitsFile(tapped.status)) return;
    const fingerprint = await this.#fingerprintOf(tapped, file);
    if (fingerprint === undefined) return;
    const row = rowAwaiting(this.#rows.all, fingerprint, tapped);
    if (row.fingerprint !== fingerprint) {
      if (row.uploadUrl !== undefined) void this.#terminate(row.uploadUrl);
      this.#rows.forgetTransfer(row);
      replaceFile(row, file, fingerprint);
    }
    requeue(row, file);
    this.#publish(row);
    this.#store.persist();
    this.#pump();
  }

  remove(id: string): void {
    const row = this.#rows.byId(id);
    if (row === undefined) return;
    if (!isSettled(row.status)) void this.cancel(id);
    this.#drop(row);
    this.#store.persistNow();
  }

  restore(): void {
    if (this.#isRestored) return;
    this.#isRestored = true;
    for (const stored of readStoredItems()) this.#push(restoredRow(stored));
  }

  applyTransfer(transfer: Transfer): void {
    const row = this.#rows.byTransferId(transfer.id);
    if (row === undefined) return;
    const outcome = settledByServer(row, transfer.status);
    if (outcome === undefined) return;
    const { upload } = row;
    this.#settle(row, outcome);
    if (outcome === 'canceled' && upload !== undefined) void upload.abort(false);
  }

  #terminate(url: string): Promise<void> {
    return uploadFactory().terminate(url).catch(leaveForServerSweep);
  }

  #push(row: Row): Row {
    this.#rows.push(row);
    this.#publish(row);
    return row;
  }

  #drop(row: Row): void {
    this.#rows.drop(row);
    this.#progressed.forget(row);
    this.#byId.delete(row.id);
  }

  async #fingerprint(row: Row): Promise<void> {
    if (row.file === undefined) return;
    const fingerprint = await this.#fingerprintOf(row, row.file);
    if (fingerprint === undefined || row.status !== 'queued') return;
    const waiting = rowAwaiting(this.#rows.all, fingerprint, row);
    if (waiting === row) {
      row.fingerprint = fingerprint;
      return;
    }
    requeue(waiting, row.file);
    this.#publish(waiting);
    this.#drop(row);
  }

  async #fingerprintOf(row: Row, file: File): Promise<string | undefined> {
    try {
      return await fingerprintFile(file);
    } catch {
      this.#settle(row, 'failed', 'file_missing');
      return undefined;
    }
  }

  #settle(row: Row, status: 'done' | 'failed' | 'canceled', error?: ErrorCode): void {
    settle(row, status, error);
    this.#publish(row);
    this.#store.persistNow();
    this.#pump();
  }

  #pump(): void {
    const ready = this.#rows.startable(Date.now());
    if (ready.length > 0 && uploadFactory().sessionId() === undefined) {
      this.#renew(undefined);
    } else {
      const slots = Math.max(0, maxParallelUploads - this.#rows.activeCount());
      for (const row of ready.slice(0, slots)) this.#start(row);
    }
    this.#watchWhileBusy();
  }

  #renew(staleId: string | undefined): void {
    if (this.#renewing !== undefined) return;
    this.#renewing = uploadFactory()
      .renewSession(staleId)
      .then((isRenewed) => {
        if (isRenewed) this.#pump();
        else this.#rows.deferQueued(Date.now() + sessionRetryMs);
        this.#watchWhileBusy();
      })
      .finally(() => {
        this.#renewing = undefined;
      });
  }

  #start(row: Row): void {
    if (row.file === undefined || row.fingerprint === undefined) return;
    if (row.uploadUrl === undefined || row.nonce === undefined) row.nonce = newUploadNonce();
    row.sealId = uploadFactory().sessionId();
    const ready = { file: row.file, fingerprint: row.fingerprint, nonce: row.nonce };
    void startAttempt(row, ready, uploadFactory().create, {
      onprogress: (sent) => {
        this.#progress(row, sent);
      },
      onurl: (url) => {
        this.#noteUrl(row, url);
      },
      onsuccess: () => {
        this.#settle(row, 'done');
      },
      onerror: (error) => {
        this.#handleError(row, error);
      },
    });
    this.#publish(row);
  }

  #progress(row: Row, sent: number): void {
    row.lastProgressAt = Date.now();
    row.sent = sent;
    if (row.status === 'stalled') row.status = 'uploading';
    this.#progressed.add(row, (rows) => {
      for (const progressed of rows) this.#publish(progressed);
    });
    this.#store.persist();
  }

  #noteUrl(row: Row, url: string): void {
    row.uploadUrl = url;
    row.transferId = transferIdOf(url);
    this.#rows.index(row);
    this.#publish(row);
    this.#store.persist();
  }

  #handleError(row: Row, error: Error | DetailedError): void {
    const failure = classify(error);
    switch (failure.status) {
      case 'failed':
        this.#fail(row, failure.code);
        return;
      case 'canceled':
        this.#settle(row, 'canceled');
        return;
      case 'stalled':
      case 'queued':
        row.upload = undefined;
        row.status = failure.status;
        row.retryAt = Date.now() + failure.delayMs;
        this.#publish(row);
        this.#pump();
    }
  }

  #fail(row: Row, code: ErrorCode): void {
    if (code === 'seal_expired' && row.file !== undefined) {
      const staleId = row.sealId;
      row.upload = undefined;
      requeue(row, row.file);
      this.#publish(row);
      this.#renew(staleId);
      return;
    }
    this.#settle(row, 'failed', code);
    if (code === 'unauthorized' || code === 'seal_invalid') session.reset();
  }

  #tick(stallThresholdMs: number): void {
    const now = Date.now();
    for (const row of this.#rows.all) {
      if (!isInFlight(row.status) || !navigator.onLine) continue;
      if (row.upload !== undefined) {
        if (now - row.lastProgressAt < stallThresholdMs) continue;
        restartAttempt(row, row.upload);
        this.#publish(row);
      } else if (now >= row.retryAt) {
        this.#start(row);
      }
    }
    this.#pump();
  }

  #watchWhileBusy(): void {
    if (this.#rows.hasWork()) {
      this.#watch();
      return;
    }
    this.#stopWatching?.();
    this.#stopWatching = undefined;
    this.#store.flush();
  }

  #watch(): void {
    this.#stopWatching ??= watchUploads({
      ontick: (stallThresholdMs) => {
        this.#tick(stallThresholdMs);
      },
      onhide: () => {
        this.#store.flush();
      },
      ononline: () => {
        this.#rows.releaseOffline();
      },
    });
  }

  #publish(row: Row): void {
    this.#byId.set(row.id, publicItem(row));
  }
}

export const uploadQueue = new UploadQueue();
