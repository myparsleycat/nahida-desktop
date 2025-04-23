<script lang="ts">
  import { Calendar as CalendarPrimitive } from "bits-ui";
  import { cn } from "$lib/utils.js";

  type $$Props = CalendarPrimitive.HeadingProps;

  let className: $$Props["class"] = undefined;
  export { className as class };

  const monthsInKorean: { [key: string]: string } = {
    January: "1월",
    February: "2월",
    March: "3월",
    April: "4월",
    May: "5월",
    June: "6월",
    July: "7월",
    August: "8월",
    September: "9월",
    October: "10월",
    November: "11월",
    December: "12월",
  };

  function translateToKorean(value: string): string {
    const [month, year] = value.split(" ");
    const koreanMonth = monthsInKorean[month];
    return `${year}년 ${koreanMonth}`;
  }
</script>

<CalendarPrimitive.Heading
  let:headingValue
  class={cn("text-sm font-medium", className)}
  {...$$restProps}
>
  <slot {headingValue}>
    {translateToKorean(headingValue)}
  </slot>
</CalendarPrimitive.Heading>
