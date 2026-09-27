import { useLayoutEffect, useRef, useState } from "react";
import { TextReveal } from "./textReveal";

export default function StreamingText({
  text,
  streaming,
  onReveal,
}: {
  text: string;
  streaming: boolean;
  onReveal?: () => void;
}) {
  const reveal = useRef<TextReveal | null>(null);
  if (!reveal.current) reveal.current = new TextReveal(text);
  const [visible, setVisible] = useState(text);
  const frame = useRef(0);
  const previousTime = useRef(0);

  useLayoutEffect(() => {
    const buffer = reveal.current!;
    const animate =
      streaming &&
      !window.matchMedia("(prefers-reduced-motion: reduce)").matches;
    buffer.update(text, animate);
    setVisible(buffer.visible);
    if (!buffer.pending) {
      cancelAnimationFrame(frame.current);
      frame.current = 0;
    } else if (!frame.current) {
      previousTime.current = performance.now();
      const tick = (now: number) => {
        setVisible(buffer.advance(now - previousTime.current));
        previousTime.current = now;
        frame.current = buffer.pending ? requestAnimationFrame(tick) : 0;
      };
      frame.current = requestAnimationFrame(tick);
    }
  }, [text, streaming]);

  useLayoutEffect(() => {
    onReveal?.();
  }, [visible, onReveal]);

  useLayoutEffect(
    () => () => {
      cancelAnimationFrame(frame.current);
      frame.current = 0;
    },
    [],
  );

  // Terminal states render the exact saved text immediately, including cancel.
  return <>{streaming ? visible : text}</>;
}
