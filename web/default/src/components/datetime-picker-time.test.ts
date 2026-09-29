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
import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import { formatTimeValue, parseTimeValue } from './datetime-picker-time'

describe('formatTimeValue', () => {
  test('pads to HH:mm when seconds are disabled', () => {
    assert.equal(
      formatTimeValue(new Date(2026, 0, 2, 9, 5, 37), false),
      '09:05'
    )
  })

  test('includes zero-padded seconds when enabled', () => {
    assert.equal(
      formatTimeValue(new Date(2026, 0, 2, 9, 5, 7), true),
      '09:05:07'
    )
  })

  test('keeps midnight stable', () => {
    assert.equal(
      formatTimeValue(new Date(2026, 0, 2, 0, 0, 0), true),
      '00:00:00'
    )
  })
})

describe('parseTimeValue', () => {
  test('parses HH:mm with seconds defaulted to zero', () => {
    assert.deepEqual(parseTimeValue('09:05'), {
      hours: 9,
      minutes: 5,
      seconds: 0,
    })
  })

  test('parses HH:mm:ss', () => {
    assert.deepEqual(parseTimeValue('23:59:59'), {
      hours: 23,
      minutes: 59,
      seconds: 59,
    })
  })

  test('falls back to zeros on malformed input', () => {
    assert.deepEqual(parseTimeValue(''), { hours: 0, minutes: 0, seconds: 0 })
    assert.deepEqual(parseTimeValue('ab:cd'), {
      hours: 0,
      minutes: 0,
      seconds: 0,
    })
  })
})
