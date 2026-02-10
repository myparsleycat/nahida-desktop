import { Button } from "@renderer/components/ui/button";
import { Calendar } from "@renderer/components/ui/calendar";
import { Popover, PopoverContent, PopoverTrigger } from "@renderer/components/ui/popover";
import { cn } from "@renderer/lib/utils";
import { formatDate } from "@shared/utils";
import { Calendar as CalendarIcon } from "lucide-react";
import type { Matcher } from "react-day-picker";

interface DatePickerProps {
  value?: Date;
  onChange: (date: Date | undefined) => void;
  disabled?: Matcher | Matcher[];
  placeholder?: string;
  className?: string;
}

export function DatePicker({ value, onChange, disabled, placeholder, className }: DatePickerProps) {
  return (
    <Popover>
      <PopoverTrigger asChild>
        <Button
          variant="outline"
          data-empty={!value}
          className={cn(
            "data-[empty=true]:text-muted-foreground w-[280px] justify-start text-left font-normal",
            className,
          )}
        >
          <CalendarIcon />
          {value ? (
            formatDate(value, navigator.language, "PPP")
          ) : (
            <span>{placeholder || "Pick a date"}</span>
          )}
        </Button>
      </PopoverTrigger>
      <PopoverContent className="w-auto p-0">
        <Calendar mode="single" selected={value} onSelect={onChange} disabled={disabled} />
      </PopoverContent>
    </Popover>
  );
}
