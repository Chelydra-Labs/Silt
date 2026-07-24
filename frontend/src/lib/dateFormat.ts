// Pure date-formatting for the Date Glance popover and /today slash command
// (#730). Maps a format-id string (stored in config.yaml editor.date_format)
// to a formatted local-date string. No settings-store dependency — callers
// read the configured value and pass it in, keeping this module testable.

export const DATE_FORMAT_IDS = [
  'YYYY-MM-DD',
  'DD-MMM-YY',
  'MM/DD/YYYY',
  'DD/MM/YYYY',
  'MMM D, YYYY',
  'long',
  'D MMM YYYY',
  'MM/DD/YY',
  'DD/MM/YY'
] as const

export type DateFormatId = (typeof DATE_FORMAT_IDS)[number]

export interface DateFormatOption {
  id: DateFormatId
  label: string
  example: string
}

export const DATE_FORMATS: DateFormatOption[] = [
  { id: 'YYYY-MM-DD', label: 'YYYY-MM-DD', example: '2026-07-24' },
  { id: 'DD-MMM-YY', label: 'DD-MMM-YY', example: '24-Jul-26' },
  { id: 'MM/DD/YYYY', label: 'MM/DD/YYYY', example: '07/24/2026' },
  { id: 'DD/MM/YYYY', label: 'DD/MM/YYYY', example: '24/07/2026' },
  { id: 'MMM D, YYYY', label: 'MMM D, YYYY', example: 'Jul 24, 2026' },
  {
    id: 'long',
    label: 'Weekday, Month D, YYYY',
    example: 'Friday, July 24, 2026'
  },
  { id: 'D MMM YYYY', label: 'D MMM YYYY', example: '24 Jul 2026' },
  { id: 'MM/DD/YY', label: 'MM/DD/YY', example: '07/24/26' },
  { id: 'DD/MM/YY', label: 'DD/MM/YY', example: '24/07/26' }
]

const MONTHS_SHORT = [
  'Jan',
  'Feb',
  'Mar',
  'Apr',
  'May',
  'Jun',
  'Jul',
  'Aug',
  'Sep',
  'Oct',
  'Nov',
  'Dec'
]
const MONTHS_LONG = [
  'January',
  'February',
  'March',
  'April',
  'May',
  'June',
  'July',
  'August',
  'September',
  'October',
  'November',
  'December'
]
const WEEKDAYS_LONG = [
  'Sunday',
  'Monday',
  'Tuesday',
  'Wednesday',
  'Thursday',
  'Friday',
  'Saturday'
]

const PAD2 = (n: number): string => String(n).padStart(2, '0')

/**
 * Format a local Date according to a format-id string. Falls back to ISO
 * (YYYY-MM-DD) for an unrecognized format so a corrupted config value never
 * produces garbage output.
 */
export function formatDate(d: Date, format: string): string {
  const y = d.getFullYear()
  const yy = String(y).slice(-2)
  const m = d.getMonth() + 1
  const mm = PAD2(m)
  const day = d.getDate()
  const dd = PAD2(day)
  const mShort = MONTHS_SHORT[d.getMonth()]
  const mLong = MONTHS_LONG[d.getMonth()]

  switch (format) {
    case 'YYYY-MM-DD':
      return `${y}-${mm}-${dd}`
    case 'DD-MMM-YY':
      return `${dd}-${mShort}-${yy}`
    case 'MM/DD/YYYY':
      return `${mm}/${dd}/${y}`
    case 'DD/MM/YYYY':
      return `${dd}/${mm}/${y}`
    case 'MMM D, YYYY':
      return `${mShort} ${day}, ${y}`
    case 'long':
      return `${WEEKDAYS_LONG[d.getDay()]}, ${mLong} ${day}, ${y}`
    case 'D MMM YYYY':
      return `${day} ${mShort} ${y}`
    case 'MM/DD/YY':
      return `${mm}/${dd}/${yy}`
    case 'DD/MM/YY':
      return `${dd}/${mm}/${yy}`
    default:
      return `${y}-${mm}-${dd}`
  }
}

/**
 * Resolve a raw config value to a valid DateFormatId. Returns the ISO default
 * for nil, empty, or unrecognized values.
 */
export function resolveDateFormat(
  configured: string | undefined | null
): DateFormatId {
  if (configured && DATE_FORMAT_IDS.includes(configured as DateFormatId)) {
    return configured as DateFormatId
  }
  return 'YYYY-MM-DD'
}
