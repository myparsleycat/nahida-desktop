import { cn } from "@renderer/lib/utils";
import { type HTMLAttributes, type ReactNode, useState } from "react";

interface CenterProps extends HTMLAttributes<HTMLDivElement> {
  children: ReactNode;
  size?: "full" | "page-full";
  className?: string;
}

export function Center({ children, size = "full", className, ...props }: CenterProps) {
  const sizeClasses = {
    full: "h-full w-full",
    "page-full": "h-dvh w-full",
  };

  return (
    <div
      className={cn(`flex items-center justify-center p-6 ${sizeClasses[size]}`, className)}
      {...props}
    >
      {children}
    </div>
  );
}

export function ServerCrash({ message }: { message?: string }) {
  return (
    <div className="flex flex-col items-center gap-2">
      <Random1619 />
      <p className="text-lg">{message || "The server is not responding"}</p>
    </div>
  );
}

const RANDOM_1619_IMAGES = [
  "/img/1619/1619-miyabi.gif",
  "/img/1619/1619-miyabi_2.gif",
  "/img/1619/1619-miyabi_3.gif",
];

interface Random1619Props {
  className?: string;
  alt?: string;
}

export function Random1619({ className, alt, ...props }: Random1619Props) {
  const [randomImage] = useState(
    () => RANDOM_1619_IMAGES[Math.floor(Math.random() * RANDOM_1619_IMAGES.length)],
  );

  return <img className={className} alt={alt} src={randomImage} {...props} />;
}

export function AlertWithRandom1619({ message }: { message: string }) {
  return (
    <div className="flex flex-col items-center justify-center space-y-4">
      <Random1619 />
      <span className="text-lg">{message}</span>
    </div>
  );
}

export * from "./loaders";
