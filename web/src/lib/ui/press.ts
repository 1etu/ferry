import { type Attachment } from 'svelte/attachments';

const releaseEvents = ['pointerup', 'pointercancel', 'pointerleave'] as const;

export function pressable(onpress: (isPressed: boolean) => void): Attachment<HTMLElement> {
  return (node) => {
    const press = (event: PointerEvent) => {
      if (event.isPrimary && event.button === 0) onpress(true);
    };
    const release = () => {
      onpress(false);
    };
    node.addEventListener('pointerdown', press);
    for (const type of releaseEvents) node.addEventListener(type, release);
    return () => {
      node.removeEventListener('pointerdown', press);
      for (const type of releaseEvents) node.removeEventListener(type, release);
    };
  };
}
