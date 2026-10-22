import { useEffect, useRef, useState } from 'react';
import mpegts from 'mpegts.js';

interface Props {
  src: string;
}

// MPEG-TS playback uses Media Source Extensions through mpegts.js. Surface
// demux/network errors in the page as well as the console so browser playback
// failures can be diagnosed without relying on a silent black video element.
export function MpegtsPlayer({ src }: Props) {
  const ref = useRef<HTMLVideoElement>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    const v = ref.current;
    if (!v) return;

    setError(null);
    const reportVideoError = () => {
      const mediaError = v.error;
      if (mediaError) {
        const message = mediaError.message || `HTML media error code ${mediaError.code}`;
        console.error('[Live TV] HTML video error', { code: mediaError.code, message, src });
        setError(message);
      }
    };
    v.addEventListener('error', reportVideoError);

    if (mpegts.getFeatureList().mseLivePlayback) {
      const player = mpegts.createPlayer({ type: 'mpegts', isLive: true, url: src });
      player.on(mpegts.Events.ERROR, (type, detail, info) => {
        const message = `MPEG-TS playback error: ${String(type)} / ${String(detail)}`;
        console.error('[Live TV] mpegts.js error', { type, detail, info, src });
        setError(message);
      });
      player.attachMediaElement(v);
      player.load();
      const playResult = player.play();
      if (playResult && typeof (playResult as Promise<void>).then === 'function') {
        (playResult as Promise<void>).catch((err: unknown) => {
          // Autoplay rejection is common and is not necessarily a stream failure.
          console.warn('[Live TV] autoplay rejected; use the video controls to start playback', err);
        });
      }
      return () => {
        v.removeEventListener('error', reportVideoError);
        player.pause();
        player.unload();
        player.detachMediaElement();
        player.destroy();
      };
    }

    v.src = src;
    return () => {
      v.removeEventListener('error', reportVideoError);
    };
  }, [src]);

  return (
    <div className="w-full">
      <video ref={ref} controls autoPlay playsInline className="aspect-video w-full bg-black" />
      {error ? (
        <div role="alert" className="border-t border-red-900/50 bg-red-950/70 px-3 py-2 text-sm text-red-200">
          {error}
          <p className="mt-1 text-xs text-red-200/80">Check the browser console and Network panel for the stream request status.</p>
        </div>
      ) : null}
    </div>
  );
}
