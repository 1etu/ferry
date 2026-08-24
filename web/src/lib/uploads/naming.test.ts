import { describe, expect, it } from 'vitest';
import { displayName } from './naming';

const now = new Date(2026, 9, 2, 14, 3, 7);

describe('displayName', () => {
  it.each([
    ['tempImageAbC123.jpg', 'image/jpeg', 'Photo 2026-10-02 14.03.07.jpg'],
    ['tempImageXyZ987.PNG', 'image/png', 'Photo 2026-10-02 14.03.07.png'],
    ['image.jpg', 'image/jpeg', 'Photo 2026-10-02 14.03.07.jpg'],
    ['tempImageQ1w2e3.mov', 'video/quicktime', 'Video 2026-10-02 14.03.07.mov'],
    ['image.MOV', 'video/quicktime', 'Video 2026-10-02 14.03.07.mov'],
  ])('renames picker temp name %s (%s)', (name, type, expected) => {
    expect(displayName({ name, type }, now)).toBe(expected);
  });

  it.each([
    ['IMG_0042.HEIC', 'image/heic'],
    ['report.pdf', 'application/pdf'],
    ['my image.jpg', 'image/jpeg'],
    ['images.jpg', 'image/jpeg'],
    ['tempImage', 'image/jpeg'],
  ])('keeps the real name %s', (name, type) => {
    expect(displayName({ name, type }, now)).toBe(name);
  });

  it('pads single-digit date and time parts', () => {
    const early = new Date(2027, 0, 5, 3, 4, 9);
    expect(displayName({ name: 'image.jpg', type: 'image/jpeg' }, early)).toBe(
      'Photo 2027-01-05 03.04.09.jpg',
    );
  });
});
