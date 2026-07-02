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

import React, { useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { API, isAdmin, showError } from '../../../helpers';
import { downloadBlobAsFile } from '../../../helpers/utils';
import BillFilters from './BillFilters';
import BillSummaryTable from './BillSummaryTable';

const PAGE_SIZE = 20;

const toUnix = (v) => {
  if (!v) return undefined;
  const ms = new Date(v).getTime();
  return Number.isNaN(ms) ? undefined : Math.floor(ms / 1000);
};

const BillSummary = () => {
  const { t } = useTranslation();
  const isAdminUser = isAdmin();
  const formApiRef = useRef(null);
  const [data, setData] = useState(null);
  const [page, setPage] = useState(1);
  const [loading, setLoading] = useState(false);

  const collectParams = () => {
    const v = formApiRef.current ? formApiRef.current.getValues() : {};
    const p = {
      start_timestamp: toUnix(v.start_time),
      end_timestamp: toUnix(v.end_time),
      token_name: v.token_name || undefined,
      model_name: v.model_name || undefined,
      exchange_rate: v.exchange_rate || undefined,
    };
    if (isAdminUser) {
      p.username = v.username || undefined;
      p.channel = v.channel || undefined;
    }
    return { values: v, params: p };
  };

  const buildQuery = (params) => {
    const s = new URLSearchParams();
    Object.entries(params).forEach(([k, val]) => {
      if (val !== undefined && val !== '' && val !== null) s.append(k, String(val));
    });
    return s;
  };

  const runQuery = async (targetPage) => {
    setLoading(true);
    try {
      const { params } = collectParams();
      const s = buildQuery(params);
      s.append('p', String(targetPage));
      s.append('page_size', String(PAGE_SIZE));
      const path = isAdminUser ? '/api/log/bill/summary' : '/api/log/self/bill/summary';
      const res = await API.get(`${path}?${s.toString()}`);
      if (res.data.success) {
        setData(res.data.data);
        setPage(targetPage);
      } else {
        showError(res.data.message);
      }
    } catch (e) {
      showError(e.message);
    } finally {
      setLoading(false);
    }
  };

  const runExport = async () => {
    try {
      const { values, params } = collectParams();
      const s = buildQuery(params);
      s.append('with_detail', values.with_detail ? '1' : '0');
      s.append('detail_split_model', values.with_detail && values.detail_split_model ? '1' : '0');
      const path = isAdminUser ? '/api/log/bill/export' : '/api/log/self/bill/export';
      const res = await API.get(`${path}?${s.toString()}`, { responseType: 'blob' });
      downloadBlobAsFile(new Blob([res.data]), `bill-summary-${Date.now()}.xlsx`);
    } catch (e) {
      showError(e.message);
    }
  };

  return (
    <div style={{ padding: 16 }}>
      <BillFilters
        formApiRef={formApiRef}
        isAdminUser={isAdminUser}
        onQuery={() => runQuery(1)}
        onExport={runExport}
        loading={loading}
        t={t}
      />
      {data && (
        <BillSummaryTable
          items={data.items}
          total={data.total}
          page={page}
          pageSize={PAGE_SIZE}
          summary={data.summary}
          isAdminUser={isAdminUser}
          onPageChange={(pg) => runQuery(pg)}
          t={t}
        />
      )}
    </div>
  );
};

export default BillSummary;
