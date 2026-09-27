const segmenter = new Intl.Segmenter(undefined, { granularity: "grapheme" });

// Presentation only: the canonical run and saved transcript remain untouched.
export class TextReveal {
  target: string;
  visible: string;
  private characters: string[] = [];
  private progress = 0;

  constructor(text: string) {
    this.target = this.visible = text;
  }

  update(text: string, streaming: boolean) {
    const appended = text.startsWith(this.target);
    if (text !== this.target) {
      this.characters = Array.from(
        segmenter.segment(text),
        (part) => part.segment,
      );
      // Existing history is already visible when a reply mounts.
      if (this.characters.length && this.target === this.visible) {
        this.progress = Array.from(segmenter.segment(this.visible)).length;
      }
      this.target = text;
    }
    if (!streaming || !appended) {
      this.visible = text;
      this.progress = this.characters.length;
    }
  }

  get pending() {
    return this.visible !== this.target;
  }

  advance(elapsedMS: number) {
    if (!this.pending) return this.visible;
    const remaining = this.characters.length - this.progress;
    // Small batches read continuously; a burst accelerates instead of building
    // a long artificial typing queue. At 60 Hz a 2,000-character burst drains
    // in under one second. Never wait for a whole word or paragraph.
    const speed = Math.max(90, remaining * 8);
    this.progress = Math.min(
      this.characters.length,
      this.progress + (speed * Math.max(0, Math.min(elapsedMS, 64))) / 1000,
    );
    this.visible = this.characters.slice(0, Math.floor(this.progress)).join("");
    return this.visible;
  }
}
