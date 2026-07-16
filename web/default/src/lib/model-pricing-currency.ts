/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/

/**
 * Pure conversions between stored, currency-neutral model pricing
 * (model_ratio dimensionless; model_price in USD) and the display/entry
 * currency. `rate` is the effective billing rate (getEffectiveBillingRate()).
 * Keeping these pure + one rate source makes the 7.3× mispricing structurally
 * impossible: display uses ×rate, entry uses ÷rate, same `rate`.
 */

/** model_ratio 1 corresponds to $2 per 1M tokens. */
export const RATIO_USD_FACTOR = 2

/** stored ratio → display price (e.g. ¥). */
export function ratioToDisplayPrice(ratio: number, rate: number): number {
  return ratio * RATIO_USD_FACTOR * rate
}

/** display price (e.g. ¥) → stored ratio. */
export function displayPriceToRatio(displayPrice: number, rate: number): number {
  return displayPrice / rate / RATIO_USD_FACTOR
}

/** per-request stored price (USD) → display price (e.g. ¥). */
export function usdPriceToDisplay(usdPrice: number, rate: number): number {
  return usdPrice * rate
}

/** per-request display price (e.g. ¥) → stored price (USD). */
export function displayToUsdPrice(displayPrice: number, rate: number): number {
  return displayPrice / rate
}
