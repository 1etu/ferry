import { type Attachment } from 'svelte/attachments';

export type SheetDrag = {
  handle: () => HTMLElement | undefined;
  onmove: (offset: number) => void;
  onrelease: () => void;
  oncancel: () => void;
};

const dragSlopPx = 4;

function isInside(element: HTMLElement | undefined, target: EventTarget | null): boolean {
  return element !== undefined && target instanceof Node && element.contains(target);
}

function startsDrag(panel: HTMLElement, distance: number, isFromHandle: boolean): boolean {
  return distance >= dragSlopPx && (isFromHandle || panel.scrollTop <= 0);
}

function leavesToScroll(panel: HTMLElement, distance: number, isFromHandle: boolean): boolean {
  if (isFromHandle) return false;
  return distance <= -dragSlopPx || (distance >= dragSlopPx && panel.scrollTop > 0);
}

function swallowClickAfterDrag(panel: HTMLElement) {
  const swallow = (event: MouseEvent) => {
    event.preventDefault();
    event.stopPropagation();
  };
  panel.addEventListener('click', swallow, { capture: true, once: true });
  setTimeout(() => {
    panel.removeEventListener('click', swallow, { capture: true });
  });
}

export function sheetDrag({
  handle,
  onmove,
  onrelease,
  oncancel,
}: SheetDrag): Attachment<HTMLElement> {
  return (panel) => {
    let startY = 0;
    let pointerId: number | undefined;
    let isFromHandle = false;
    let isDragging = false;
    let touchStartY = 0;
    let isTouchFromHandle = false;

    const move = (event: PointerEvent) => {
      if (event.pointerId !== pointerId) return;
      const distance = event.clientY - startY;
      if (!isDragging) {
        if (leavesToScroll(panel, distance, isFromHandle)) {
          stopTracking();
          return;
        }
        if (!startsDrag(panel, distance, isFromHandle)) return;
        isDragging = true;
      }
      onmove(Math.max(0, distance));
    };

    const finish = (event: PointerEvent) => {
      if (event.pointerId !== pointerId) return;
      stopTracking();
      if (!isDragging) return;
      isDragging = false;
      swallowClickAfterDrag(panel);
      onrelease();
    };

    const cancel = (event: PointerEvent) => {
      if (event.pointerId !== pointerId) return;
      stopTracking();
      isDragging = false;
      oncancel();
    };

    const stopTracking = () => {
      pointerId = undefined;
      window.removeEventListener('pointermove', move);
      window.removeEventListener('pointerup', finish);
      window.removeEventListener('pointercancel', cancel);
    };

    const start = (event: PointerEvent) => {
      if (!event.isPrimary || event.button !== 0) return;
      for (const animation of panel.getAnimations()) animation.finish();
      pointerId = event.pointerId;
      startY = event.clientY;
      isFromHandle = isInside(handle(), event.target);
      window.addEventListener('pointermove', move);
      window.addEventListener('pointerup', finish);
      window.addEventListener('pointercancel', cancel);
    };

    const noteTouch = (event: TouchEvent) => {
      touchStartY = event.touches[0]?.clientY ?? 0;
      isTouchFromHandle = isInside(handle(), event.target);
    };

    const holdScrollWhileDragging = (event: TouchEvent) => {
      const touch = event.touches[0];
      if (!event.cancelable || touch === undefined || event.touches.length > 1) return;
      const distance = touch.clientY - touchStartY;
      if (isDragging || startsDrag(panel, distance, isTouchFromHandle)) event.preventDefault();
    };

    panel.addEventListener('pointerdown', start);
    panel.addEventListener('touchstart', noteTouch, { passive: true });
    panel.addEventListener('touchmove', holdScrollWhileDragging, { passive: false });
    return () => {
      panel.removeEventListener('pointerdown', start);
      panel.removeEventListener('touchstart', noteTouch);
      panel.removeEventListener('touchmove', holdScrollWhileDragging);
      stopTracking();
    };
  };
}
