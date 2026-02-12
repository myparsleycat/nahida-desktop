import { motion, type Variants } from "framer-motion";
import { Loader2Icon } from "lucide-react";
import Alice1619 from "@/renderer/assets/img/1619/1619-alice.gif";

export function LoaderSpinner() {
  return <Loader2Icon size={70} className="animate-spin" />;
}

export const BarLoader = () => {
  const variants: Variants = {
    initial: {
      scaleY: 0.5,
      opacity: 0,
    },
    animate: {
      scaleY: 1,
      opacity: 1,
      transition: {
        repeat: Infinity,
        repeatType: "mirror",
        duration: 1,
        ease: "circIn",
      },
    },
  };

  return (
    <motion.div
      transition={{
        staggerChildren: 0.25,
      }}
      initial="initial"
      animate="animate"
      className="flex gap-1"
    >
      <motion.div variants={variants} className="h-12 w-2 bg-white" />
      <motion.div variants={variants} className="h-12 w-2 bg-white" />
      <motion.div variants={variants} className="h-12 w-2 bg-white" />
      <motion.div variants={variants} className="h-12 w-2 bg-white" />
      <motion.div variants={variants} className="h-12 w-2 bg-white" />
    </motion.div>
  );
};

export function AliceLoader() {
  return <img src={Alice1619} height={250} width={250} />;
}

export function RandomLoader() {
  const components = [<AliceLoader />];

  const randomIndex = Math.floor(Math.random() * components.length);

  return components[randomIndex];
}
