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

import React, { useCallback, useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  Button,
  Card,
  Empty,
  Form,
  Modal,
  Table,
  TabPane,
  Tabs,
  Tag,
  Typography,
} from '@douyinfe/semi-ui';
import { IconSearch } from '@douyinfe/semi-icons';
import {
  IllustrationNoResult,
  IllustrationNoResultDark,
} from '@douyinfe/semi-illustrations';
import { CARD_PROPS } from '../../constants/dashboard.constants';
import { DATE_RANGE_PRESETS } from '../../constants/console.constants';
import {
  copy,
  isRoot,
  showError,
  showSuccess,
  timestamp2string,
} from '../../helpers';
import {
  deleteVolcAsset,
  getAllVolcAssets,
  getSelfVolcAssetQuota,
  getSelfVolcAssets,
  getVolcAssetQuota,
  syncVolcAssets,
} from './api';

const { Text } = Typography;

const PAGE_SIZE = 20;

/** 官方素材状态。Processing 表示上传后仍在转码，此时还不能用 asset:// 引用。 */
const STATUS_TAGS = {
  Active: { color: 'green', label: '可用' },
  Processing: { color: 'orange', label: '处理中' },
  Failed: { color: 'red', label: '失败' },
};

/** 同步时认领无主行写入的 user_id，与后端 model.VolcAssetOrphanOwnerId 对齐。 */
const ORPHAN_OWNER_ID = 0;

/** Semi 的 dateTimeRange 给的是格式化字符串，后端要的是秒。 */
function toSeconds(value) {
  if (!value) return 0;
  const ms = Date.parse(value);
  return Number.isFinite(ms) ? Math.floor(ms / 1000) : 0;
}

function QuotaItem({ label, value, type }) {
  return (
    <div className='flex items-baseline gap-1'>
      <Text type='tertiary' size='small'>
        {label}
      </Text>
      <Text
        type={type}
        style={{ fontFamily: 'monospace', fontWeight: 600 }}
        size='small'
      >
        {value}
      </Text>
    </div>
  );
}

/**
 * 客户视角的额度，横在表格上方。
 *
 * 不做成独立页签：额度是看表格时的参照物（还能建几条），拆到另一个页签
 * 就得来回切。上限为 0 表示站点没设限，此时只报已用量 —— 画一个「12 / 0」
 * 既难看也没意义。
 */
function SelfQuotaBar({ t, refreshToken }) {
  const [quota, setQuota] = useState(null);

  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const res = await getSelfVolcAssetQuota();
        if (cancelled || !res.success) return;
        setQuota(res.data || null);
      } catch (error) {
        if (!cancelled) showError(error);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [refreshToken]);

  if (!quota) return null;

  const limited = quota.limit > 0;
  const remaining = limited ? Math.max(quota.limit - quota.used, 0) : null;

  return (
    <div className='flex flex-wrap items-center gap-4'>
      <QuotaItem
        label={t('已用资产')}
        value={limited ? `${quota.used} / ${quota.limit}` : `${quota.used}`}
      />
      {limited && (
        <QuotaItem
          label={t('剩余')}
          value={`${remaining}`}
          type={remaining === 0 ? 'danger' : 'success'}
        />
      )}
      <Text type='tertiary' size='small'>
        {quota.enforced
          ? t('达到上限后将拒绝创建新资产。')
          : t('当前上限仅作参考，不会拦截创建。')}
      </Text>
    </div>
  );
}

/**
 * 超管视角的额度，按渠道对账。
 *
 * 孤儿数与上游未记账分开报：前者靠认领或删除清理，后者靠跑同步消化，
 * 处置完全不同，合成一个数就没法判断该做哪件事。
 */
function ChannelQuotaBar({ t, refreshToken }) {
  const [items, setItems] = useState([]);

  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const res = await getVolcAssetQuota();
        if (cancelled || !res.success) return;
        setItems(res.data?.items || []);
      } catch (error) {
        if (!cancelled) showError(error);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [refreshToken]);

  if (items.length === 0) return null;

  return (
    <div className='flex flex-col gap-1'>
      {items.map((item) => (
        <div
          key={item.channel_id}
          className='flex flex-wrap items-center gap-4'
        >
          <Text size='small' strong>
            {`${item.channel_name || t('渠道')} #${item.channel_id}`}
          </Text>
          <QuotaItem
            label={t('上游已用')}
            // 官方没有查素材限额的接口，上限是管理员在渠道里手填的值。
            value={
              item.quota_limit > 0
                ? `${item.upstream_assets + item.upstream_groups} / ${item.quota_limit}`
                : `${item.upstream_assets + item.upstream_groups}`
            }
          />
          <QuotaItem
            label={t('本地记账')}
            value={`${item.local_assets + item.local_groups}`}
          />
          {(item.orphan_assets + item.orphan_groups > 0 ||
            item.unrecorded > 0) && (
            <QuotaItem
              label={t('孤儿 / 未记账')}
              value={`${item.orphan_assets + item.orphan_groups} / ${item.unrecorded}`}
              type='warning'
            />
          )}
          {item.error && (
            <Text type='danger' size='small'>
              {item.error}
            </Text>
          )}
        </div>
      ))}
    </div>
  );
}

function AssetTable({ admin, t, refreshToken }) {
  const [assets, setAssets] = useState([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [loading, setLoading] = useState(false);
  const [formApi, setFormApi] = useState(null);
  // 点了查询才落到这里：日期范围每拖一下就发一次请求既慢又吵。
  const [query, setQuery] = useState({});
  const [deleting, setDeleting] = useState(null);

  const load = useCallback(
    async (targetPage, activeQuery) => {
      setLoading(true);
      try {
        const params = {
          p: targetPage,
          pageSize: PAGE_SIZE,
          ...activeQuery,
        };
        const res = admin
          ? await getAllVolcAssets(params)
          : await getSelfVolcAssets(params);
        if (!res.success) {
          showError(res.message);
          return;
        }
        setAssets(res.data?.items || []);
        setTotal(res.data?.total || 0);
      } catch (error) {
        showError(error);
      } finally {
        setLoading(false);
      }
    },
    [admin],
  );

  useEffect(() => {
    load(page, query);
  }, [load, page, query, refreshToken]);

  const handleSearch = () => {
    const values = formApi ? formApi.getValues() : {};
    const range = Array.isArray(values.dateRange) ? values.dateRange : [];
    // 条件变了就回第一页，否则第 3 页筛出 5 条会显示空表。
    setPage(1);
    setQuery({
      resourceId: values.resource_id || '',
      resourceType: values.resource_type || '',
      status: values.status || '',
      keyword: admin ? values.keyword || '' : '',
      userId: admin ? values.user_id || '' : '',
      channelId: admin ? values.channel_id || '' : '',
      startTimestamp: toSeconds(range[0]),
      endTimestamp: toSeconds(range[1]),
    });
  };

  const handleReset = () => {
    if (!formApi) return;
    formApi.reset();
    setPage(1);
    setQuery({});
  };

  const handleCopy = async (value) => {
    if (await copy(value)) {
      showSuccess(t('已复制到剪贴板'));
    } else {
      showError(t('复制失败'));
    }
  };

  const handleDelete = async (record) => {
    setDeleting(record.id);
    try {
      const res = await deleteVolcAsset(record.id);
      if (!res.success) {
        showError(res.message);
        return;
      }
      showSuccess(t('资产删除成功'));
      await load(page, query);
    } catch (error) {
      showError(error);
    } finally {
      setDeleting(null);
    }
  };

  const confirmDelete = (record) => {
    Modal.confirm({
      title: t('确定要删除此资产吗？'),
      content: t('将先删除上游素材，成功后再移除本地记录。此操作不可逆。'),
      type: 'warning',
      onOk: () => handleDelete(record),
    });
  };

  const columns = useMemo(() => {
    const base = [
      {
        title: t('资产 ID'),
        dataIndex: 'resource_id',
        render: (value) =>
          value ? (
            <Text
              link
              onClick={() => handleCopy(value)}
              style={{ fontFamily: 'monospace' }}
            >
              {value}
            </Text>
          ) : (
            '-'
          ),
      },
      {
        title: t('名称'),
        dataIndex: 'name',
        render: (value) => value || '-',
      },
      {
        title: t('资源类型'),
        dataIndex: 'resource_type',
        render: (value) => (
          <Tag
            color={value === 'asset_group' ? 'purple' : 'blue'}
            shape='circle'
          >
            {value === 'asset_group' ? t('素材组') : t('素材')}
          </Tag>
        ),
      },
      {
        title: t('素材类型'),
        dataIndex: 'asset_type',
        render: (value) => value || '-',
      },
      {
        title: t('所属组'),
        dataIndex: 'group_id',
        render: (value) =>
          value ? (
            <Text style={{ fontFamily: 'monospace' }}>{value}</Text>
          ) : (
            '-'
          ),
      },
      {
        title: t('状态'),
        dataIndex: 'status',
        render: (value) => {
          const tag = STATUS_TAGS[value];
          if (!tag) return value || '-';
          return (
            <Tag color={tag.color} shape='circle'>
              {t(tag.label)}
            </Tag>
          );
        },
      },
      {
        title: t('创建时间'),
        dataIndex: 'created_at',
        render: (value) => (value ? timestamp2string(value) : '-'),
      },
    ];

    if (!admin) {
      return base;
    }

    base.unshift(
      {
        title: t('用户'),
        dataIndex: 'user_id',
        render: (value, record) =>
          value === ORPHAN_OWNER_ID ? (
            <Tag color='orange' shape='circle'>
              {t('孤儿')}
            </Tag>
          ) : (
            record.username || `#${value}`
          ),
      },
      {
        title: t('渠道'),
        dataIndex: 'channel_id',
      },
    );

    base.push({
      title: '',
      dataIndex: 'operate',
      render: (_, record) => (
        <Button
          type='danger'
          theme='light'
          size='small'
          loading={deleting === record.id}
          onClick={() => confirmDelete(record)}
        >
          {t('删除')}
        </Button>
      ),
    });

    return base;
  }, [admin, deleting, t]);

  return (
    <div className='flex flex-col gap-3'>
      <Form
        getFormApi={(api) => setFormApi(api)}
        onSubmit={handleSearch}
        allowEmpty={true}
        autoComplete='off'
        layout='vertical'
        trigger='change'
        stopValidateWithError={false}
      >
        <div className='flex flex-col gap-2'>
          <div className='grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-2'>
            <div className='col-span-1 lg:col-span-2'>
              <Form.DatePicker
                field='dateRange'
                className='w-full'
                type='dateTimeRange'
                placeholder={[t('开始时间'), t('结束时间')]}
                showClear
                pure
                size='small'
                presets={DATE_RANGE_PRESETS.map((preset) => ({
                  text: t(preset.text),
                  start: preset.start(),
                  end: preset.end(),
                }))}
              />
            </div>

            <Form.Input
              field='resource_id'
              prefix={<IconSearch />}
              placeholder={t('资产 ID')}
              showClear
              pure
              size='small'
            />

            <Form.Select
              field='resource_type'
              placeholder={t('资源类型')}
              className='w-full'
              showClear
              pure
              size='small'
              optionList={[
                { label: t('素材'), value: 'asset' },
                { label: t('素材组'), value: 'asset_group' },
              ]}
            />

            <Form.Select
              field='status'
              placeholder={t('状态')}
              className='w-full'
              showClear
              pure
              size='small'
              optionList={[
                { label: t('可用'), value: 'Active' },
                { label: t('处理中'), value: 'Processing' },
                { label: t('失败'), value: 'Failed' },
              ]}
            />

            {/* 关键字与用户/渠道只对超管有意义：/self 的归属由认证上下文写死 */}
            {admin && (
              <>
                <Form.Input
                  field='keyword'
                  prefix={<IconSearch />}
                  placeholder={t('名称关键字')}
                  showClear
                  pure
                  size='small'
                />
                <Form.Input
                  field='user_id'
                  prefix={<IconSearch />}
                  placeholder={t('用户 ID')}
                  showClear
                  pure
                  size='small'
                />
                <Form.Input
                  field='channel_id'
                  prefix={<IconSearch />}
                  placeholder={t('渠道 ID')}
                  showClear
                  pure
                  size='small'
                />
              </>
            )}
          </div>

          <div className='flex justify-between items-center'>
            <div></div>
            <div className='flex gap-2'>
              <Button
                type='tertiary'
                htmlType='submit'
                loading={loading}
                size='small'
              >
                {t('查询')}
              </Button>
              <Button type='tertiary' onClick={handleReset} size='small'>
                {t('重置')}
              </Button>
            </div>
          </div>
        </div>
      </Form>

      {/* 额度横在表格上方，跟表格同屏 */}
      {admin ? (
        <ChannelQuotaBar t={t} refreshToken={refreshToken} />
      ) : (
        <SelfQuotaBar t={t} refreshToken={refreshToken} />
      )}

      <Table
        columns={columns}
        dataSource={assets}
        loading={loading}
        rowKey='id'
        empty={
          <Empty
            image={<IllustrationNoResult style={{ width: 150, height: 150 }} />}
            darkModeImage={
              <IllustrationNoResultDark style={{ width: 150, height: 150 }} />
            }
            description={t('暂无资产')}
          />
        }
        pagination={{
          currentPage: page,
          pageSize: PAGE_SIZE,
          total,
          onPageChange: setPage,
        }}
      />
    </div>
  );
}

const VolcAssets = () => {
  const { t } = useTranslation();
  // 门槛取 isRoot()：「全部资产」与回源同步对应的后端路由挂的是 RootAuth()，
  // 那些路径能看到全站客户的素材、能删上游资源，管理员权限不足以承担。
  const root = isRoot();
  const [syncing, setSyncing] = useState(false);
  const [refreshToken, setRefreshToken] = useState(0);

  const handleSync = async () => {
    setSyncing(true);
    try {
      const res = await syncVolcAssets();
      if (!res.success) {
        showError(res.message);
        return;
      }
      const list = res.data?.items || [];
      const claimed = list.reduce((sum, item) => sum + (item.claimed || 0), 0);
      const refreshed = list.reduce(
        (sum, item) => sum + (item.refreshed || 0),
        0,
      );
      showSuccess(
        `${t('同步完成')} · ${t('认领')} ${claimed} · ${t('刷新')} ${refreshed}`,
      );
      list
        .filter((item) => item.error)
        .forEach((item) => showError(`#${item.channel_id}: ${item.error}`));
      setRefreshToken((prev) => prev + 1);
    } catch (error) {
      showError(error);
    } finally {
      setSyncing(false);
    }
  };

  return (
    <div className='mt-[60px] px-2'>
      <Card
        {...CARD_PROPS}
        title={t('素材库')}
        headerExtraContent={
          root ? (
            <Button loading={syncing} onClick={handleSync}>
              {t('回源同步')}
            </Button>
          ) : null
        }
      >
        {root ? (
          <Tabs type='line'>
            <TabPane tab={t('我的资产')} itemKey='self'>
              <AssetTable admin={false} t={t} refreshToken={refreshToken} />
            </TabPane>
            <TabPane tab={t('全部资产')} itemKey='all'>
              <AssetTable admin t={t} refreshToken={refreshToken} />
            </TabPane>
          </Tabs>
        ) : (
          <AssetTable admin={false} t={t} refreshToken={refreshToken} />
        )}
      </Card>
    </div>
  );
};

export default VolcAssets;
