import { cn } from "@renderer/lib/utils";
import { AnimatePresence, motion } from "framer-motion";
import React, {
  createContext,
  type ReactNode,
  useContext,
  useEffect,
  useRef,
  useState,
} from "react";

type TerminalRootProps = {
  delay?: number;
  speed?: number;
  onComplete?: () => void;
  children: ReactNode;
  className?: string;
};

type TerminalContextType = {
  speed: number;
  active: boolean;
  registerAnimation: (delay: number) => void;
  completeAnimation: (delay: number) => void;
};

const TerminalContext = createContext<TerminalContextType | undefined>(undefined);
const LoopContext = createContext<{ triggerNext: () => void } | undefined>(undefined);

export function TerminalRoot({
  delay = 0,
  speed = 1,
  onComplete = () => {},
  children,
  className,
}: TerminalRootProps) {
  const [active, setActive] = useState(false);
  const [registeredDelays, setRegisteredDelays] = useState<number[]>([]);
  const [completedDelays, setCompletedDelays] = useState<number[]>([]);
  const loop = useContext(LoopContext);

  const registerAnimation = (d: number) => {
    setRegisteredDelays((prev) => [...prev, d]);
  };

  const completeAnimation = (d: number) => {
    setCompletedDelays((prev) => {
      const next = [...prev, d];
      if (next.length === registeredDelays.length && registeredDelays.length > 0) {
        onComplete();
        loop?.triggerNext();
      }
      return next;
    });
  };

  useEffect(() => {
    const timer = setTimeout(() => {
      setActive(true);
    }, delay);
    return () => clearTimeout(timer);
  }, [delay]);

  return (
    <TerminalContext.Provider value={{ speed, active, registerAnimation, completeAnimation }}>
      <div
        className={cn(
          "font-mono text-sm font-light rounded-lg border bg-card text-card-foreground shadow-sm overflow-hidden",
          className,
        )}
      >
        <div className="flex items-center gap-1.5 border-b bg-muted/50 px-4 py-2">
          <div className="h-2 w-2 rounded-full bg-red-500" />
          <div className="h-2 w-2 rounded-full bg-amber-500" />
          <div className="h-2 w-2 rounded-full bg-emerald-500" />
        </div>
        <div className="p-4">{children}</div>
      </div>
    </TerminalContext.Provider>
  );
}

type TerminalAnimationProps = {
  children: ReactNode;
  delay?: number;
  className?: string;
};

export function TerminalAnimation({ children, delay = 0, className }: TerminalAnimationProps) {
  const context = useContext(TerminalContext);
  const [shouldPlay, setShouldPlay] = useState(false);

  // Hooks must run regardless of context existence.
  // Use defaults if context is missing to prevent crashes during Hook execution.
  const speed = context?.speed ?? 1;
  const active = context?.active ?? false;
  const duration = 0.3 / speed;

  useEffect(() => {
    if (context) {
      context.registerAnimation(delay);
    }
  }, [context, delay]);

  useEffect(() => {
    if (active && context) {
      const timer = setTimeout(() => {
        setShouldPlay(true);
        setTimeout(() => context.completeAnimation(delay), duration * 1000);
      }, delay / speed);
      return () => clearTimeout(timer);
    }
  }, [active, speed, delay, context, duration]);

  if (!context) return null;

  return (
    <AnimatePresence>
      {shouldPlay && (
        <motion.span
          initial={{ opacity: 0, y: -5 }}
          animate={{ opacity: 1, y: 0 }}
          transition={{ duration }}
          className={cn("block", className)}
        >
          {children}
        </motion.span>
      )}
    </AnimatePresence>
  );
}

type TerminalLoadingProps = {
  loadingMessage: ReactNode;
  completeMessage: ReactNode;
  delay?: number;
  duration?: number;
  className?: string;
};

export function TerminalLoading({
  loadingMessage,
  completeMessage,
  delay = 0,
  duration = 1000,
  className,
}: TerminalLoadingProps) {
  const context = useContext(TerminalContext);
  const [shouldPlay, setShouldPlay] = useState(false);
  const [complete, setComplete] = useState(false);
  const [frameIndex, setFrameIndex] = useState(0);

  const speed = context?.speed ?? 1;
  const active = context?.active ?? false;

  const frames = ["◒", "◐", "◓", "◑"];
  const flyDuration = 0.3 / speed;

  useEffect(() => {
    if (context) {
      context.registerAnimation(delay);
    }
  }, [context, delay]);

  useEffect(() => {
    if (active && context) {
      const timer = setTimeout(() => {
        setShouldPlay(true);
        const interval = setInterval(() => {
          setFrameIndex((prev) => (prev >= frames.length - 1 ? 0 : prev + 1));
        }, 75 / speed);

        setTimeout(() => {
          clearInterval(interval);
          setComplete(true);
          context.completeAnimation(delay);
        }, duration / speed);
      }, delay / speed);
      return () => clearTimeout(timer);
    }
  }, [active, speed, delay, duration, context]);

  if (!context) return null;

  return (
    <AnimatePresence>
      {shouldPlay && (
        <motion.span
          initial={{ opacity: 0, y: -5 }}
          animate={{ opacity: 1, y: 0 }}
          transition={{ duration: flyDuration }}
          className={cn("block", className)}
        >
          {!complete ? (
            <>
              <span className="text-cyan-400 mr-2">{frames[frameIndex]}</span>
              {loadingMessage}
            </>
          ) : (
            <span data-completed>{completeMessage}</span>
          )}
        </motion.span>
      )}
    </AnimatePresence>
  );
}

export function TerminalTypewriter({ children, delay = 0, className }: TerminalAnimationProps) {
  const context = useContext(TerminalContext);
  const [shouldPlay, setShouldPlay] = useState(false);

  const speed = context?.speed ?? 1;
  const active = context?.active ?? false;

  useEffect(() => {
    if (context) {
      context.registerAnimation(delay);
    }
  }, [context, delay]);

  useEffect(() => {
    if (active && context) {
      const timer = setTimeout(() => {
        setShouldPlay(true);
      }, delay / speed);
      return () => clearTimeout(timer);
    }
  }, [active, speed, delay, context]);

  if (!context) return null;

  return (
    <AnimatePresence>
      {shouldPlay && (
        <motion.span
          className={cn("block overflow-hidden whitespace-nowrap", className)}
          initial={{ width: 0 }}
          animate={{ width: "100%" }}
          transition={{
            duration: 1 / speed,
            ease: "linear",
          }}
          onAnimationComplete={() => context.completeAnimation(delay)}
        >
          {children}
        </motion.span>
      )}
    </AnimatePresence>
  );
}

type TerminalLoopProps = {
  delay?: number;
  children: ReactNode;
};

export function TerminalLoop({ delay = 500, children }: TerminalLoopProps) {
  const [loopIndex, setLoopIndex] = useState(0);
  const timeoutRef = useRef<ReturnType<typeof setTimeout>>(null);

  const triggerNext = () => {
    timeoutRef.current = setTimeout(() => {
      setLoopIndex((prev) => prev + 1);
    }, delay);
  };

  useEffect(() => {
    return () => {
      if (timeoutRef.current) {
        clearTimeout(timeoutRef.current);
      }
    };
  }, []);

  return (
    <LoopContext.Provider value={{ triggerNext }}>
      <React.Fragment key={loopIndex}>{children}</React.Fragment>
    </LoopContext.Provider>
  );
}
