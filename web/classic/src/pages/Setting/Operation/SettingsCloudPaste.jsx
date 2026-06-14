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

import React, { useEffect, useState, useRef } from 'react';
import { Button, Col, Form, Row, Spin, Typography } from '@douyinfe/semi-ui';
import {
  compareObjects,
  API,
  showError,
  showSuccess,
  showWarning,
} from '../../../helpers';
import { useTranslation } from 'react-i18next';

export default function SettingsCloudPaste(props) {
  const { t } = useTranslation();
  const [loading, setLoading] = useState(false);
  const [inputs, setInputs] = useState({
    CloudPasteEnabled: false,
    CloudPasteAutoTransfer: false,
    CloudPasteBaseURL: '',
    CloudPasteAPIKey: '',
    CloudPasteStorageConfigID: '',
  });
  const refForm = useRef();
  const [inputsRow, setInputsRow] = useState(inputs);

  function handleFieldChange(fieldName) {
    return (value) => {
      setInputs((prev) => ({ ...prev, [fieldName]: value }));
    };
  }

  function onSubmit() {
    const updateArray = compareObjects(inputs, inputsRow);
    if (!updateArray.length) return showWarning(t('你似乎并没有修改什么'));
    const requestQueue = updateArray.map((item) => {
      let value = '';
      if (typeof inputs[item.key] === 'boolean') {
        value = String(inputs[item.key]);
      } else {
        value = String(inputs[item.key]);
      }
      return API.put('/api/option/', {
        key: item.key,
        value,
      });
    });
    setLoading(true);
    Promise.all(requestQueue)
      .then((res) => {
        if (requestQueue.length === 1) {
          if (res.includes(undefined)) return;
        } else if (requestQueue.length > 1) {
          if (res.includes(undefined))
            return showError(t('部分保存失败，请重试'));
        }
        showSuccess(t('保存成功'));
        props.refresh();
      })
      .catch(() => {
        showError(t('保存失败，请重试'));
      })
      .finally(() => {
        setLoading(false);
      });
  }

  useEffect(() => {
    const defaults = {
      CloudPasteEnabled: false,
      CloudPasteAutoTransfer: false,
      CloudPasteBaseURL: '',
      CloudPasteAPIKey: '',
      CloudPasteStorageConfigID: '',
    };
    const currentInputs = { ...defaults };
    for (let key in props.options) {
      if (Object.keys(defaults).includes(key)) {
        if (key === 'CloudPasteAPIKey') {
          if (
            props.options[key] &&
            props.options[key].includes('***')
          ) {
            currentInputs[key] = '';
          } else {
            currentInputs[key] = props.options[key] || '';
          }
        } else {
          currentInputs[key] = props.options[key];
        }
      }
    }
    setInputs(currentInputs);
    setInputsRow(structuredClone(currentInputs));
    refForm.current.setValues(currentInputs);
  }, [props.options]);

  return (
    <>
      <Spin spinning={loading}>
        <Form
          values={inputs}
          getFormApi={(formAPI) => (refForm.current = formAPI)}
          style={{ marginBottom: 15 }}
        >
          <Form.Section text={t('媒体转存设置（CloudPaste）')}>
            <Typography.Text
              type='tertiary'
              style={{ marginBottom: 16, display: 'block' }}
            >
              {t(
                '配置 CloudPaste 服务以将 AI 生成的图片等媒体文件自动转存到您的存储服务',
              )}
            </Typography.Text>
            <Row gutter={16}>
              <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                <Form.Switch
                  field={'CloudPasteEnabled'}
                  label={t('启用 CloudPaste 转存')}
                  size='default'
                  checkedText='｜'
                  uncheckedText='〇'
                  onChange={handleFieldChange('CloudPasteEnabled')}
                />
              </Col>
              <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                <Form.Switch
                  field={'CloudPasteAutoTransfer'}
                  label={t('自动转存')}
                  size='default'
                  checkedText='｜'
                  uncheckedText='〇'
                  onChange={handleFieldChange('CloudPasteAutoTransfer')}
                  disabled={!inputs.CloudPasteEnabled}
                />
              </Col>
            </Row>
            <Row gutter={16}>
              <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                <Form.Input
                  field={'CloudPasteBaseURL'}
                  label={t('CloudPaste 服务地址')}
                  placeholder={'https://cp.example.com'}
                  onChange={handleFieldChange('CloudPasteBaseURL')}
                  disabled={!inputs.CloudPasteEnabled}
                />
              </Col>
              <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                <Form.Input
                  field={'CloudPasteAPIKey'}
                  label={t('API Key')}
                  placeholder={t('输入 CloudPaste API Key')}
                  mode='password'
                  onChange={handleFieldChange('CloudPasteAPIKey')}
                  disabled={!inputs.CloudPasteEnabled}
                />
              </Col>
              <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                <Form.Input
                  field={'CloudPasteStorageConfigID'}
                  label={t('存储配置 ID')}
                  placeholder={t('留空使用默认存储')}
                  onChange={handleFieldChange('CloudPasteStorageConfigID')}
                  disabled={!inputs.CloudPasteEnabled}
                />
              </Col>
            </Row>
            <Row>
              <Button size='default' onClick={onSubmit}>
                {t('保存媒体转存设置')}
              </Button>
            </Row>
          </Form.Section>
        </Form>
      </Spin>
    </>
  );
}
