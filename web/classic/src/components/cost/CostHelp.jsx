/*
Copyright (C) 2025 QuantumNous

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

import React from 'react';
import { Popover } from '@douyinfe/semi-ui';
import { IconHelpCircle } from '@douyinfe/semi-icons';

/**
 * 列名后的「?」标记：鼠标悬浮解释这一列是怎么算出来的。
 * 只用在派生列（利润率、成本倍率、分组折扣）上——这些列的数字不是后端原始字段，
 * 不解释口径管理员就得自己猜公式。
 */
export const CostHelpHeader = ({ title, children, width = 300 }) => (
  <span style={{ display: 'inline-flex', alignItems: 'center', gap: 4 }}>
    {title}
    <Popover
      showArrow
      trigger='hover'
      position='top'
      content={
        <div
          style={{
            maxWidth: width,
            padding: '8px 4px',
            fontWeight: 400,
            textAlign: 'left',
            lineHeight: 1.6,
          }}
        >
          {children}
        </div>
      }
    >
      <IconHelpCircle
        style={{
          color: 'var(--semi-color-text-2)',
          cursor: 'help',
          fontSize: 13,
        }}
      />
    </Popover>
  </span>
);

/** 悬浮层里的一条「名词 = 算式」。 */
export const CostHelpFormula = ({ term, expression }) => (
  <div style={{ marginBottom: 6 }}>
    <div style={{ fontWeight: 600 }}>{term}</div>
    <div style={{ color: 'var(--semi-color-text-2)' }}>{expression}</div>
  </div>
);

/** 悬浮层里的注意事项（回答「为什么对不上」）。 */
export const CostHelpNotes = ({ notes }) => (
  <ul
    style={{
      margin: '4px 0 0',
      paddingLeft: 16,
      color: 'var(--semi-color-text-2)',
    }}
  >
    {notes.map((note) => (
      <li key={note} style={{ marginBottom: 2 }}>
        {note}
      </li>
    ))}
  </ul>
);

export default CostHelpHeader;
