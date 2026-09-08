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
import { Button, Col, Form, Row, Spin } from '@douyinfe/semi-ui';
import {
  compareObjects,
  API,
  showError,
  showSuccess,
  showWarning,
  parseHttpStatusCodeRules,
  toBoolean,
} from '../../../helpers';
import { useTranslation } from 'react-i18next';
import HttpStatusCodeRulesInput from '../../../components/settings/HttpStatusCodeRulesInput';

const defaultMonitoringInputs = {
  ChannelDisableThreshold: '',
  QuotaRemindThreshold: '',
  AutomaticDisableChannelEnabled: false,
  AutomaticEnableChannelEnabled: false,
  AutomaticDisableKeywords: '',
  AutomaticDisableStatusCodes: '401',
  AutomaticRetryStatusCodes:
    '100-199,300-399,401-407,409-499,500-503,505-523,525-599',
  'monitor_setting.auto_test_channel_enabled': false,
  'monitor_setting.auto_test_channel_minutes': 10,
  'monitor_setting.dynamic_channel_weight_enabled': false,
  'monitor_setting.dynamic_channel_weight_window_minutes': 15,
  'monitor_setting.dynamic_channel_weight_min_samples': 20,
  'monitor_setting.dynamic_channel_weight_target_frt_ms': 8000,
  'monitor_setting.dynamic_channel_weight_error_penalty': 1,
  'monitor_setting.dynamic_channel_weight_429_penalty': 1.5,
  'monitor_setting.dynamic_channel_weight_min_multiplier': 0.25,
  'monitor_setting.dynamic_channel_weight_max_multiplier': 2,
};

const numericMonitoringKeys = new Set([
  'ChannelDisableThreshold',
  'QuotaRemindThreshold',
  'monitor_setting.auto_test_channel_minutes',
  'monitor_setting.dynamic_channel_weight_window_minutes',
  'monitor_setting.dynamic_channel_weight_min_samples',
  'monitor_setting.dynamic_channel_weight_target_frt_ms',
  'monitor_setting.dynamic_channel_weight_error_penalty',
  'monitor_setting.dynamic_channel_weight_429_penalty',
  'monitor_setting.dynamic_channel_weight_min_multiplier',
  'monitor_setting.dynamic_channel_weight_max_multiplier',
]);

const booleanMonitoringKeys = new Set([
  'AutomaticDisableChannelEnabled',
  'AutomaticEnableChannelEnabled',
  'monitor_setting.auto_test_channel_enabled',
  'monitor_setting.dynamic_channel_weight_enabled',
]);

function normalizeMonitoringValue(key, value) {
  if (booleanMonitoringKeys.has(key)) return toBoolean(value);
  if (numericMonitoringKeys.has(key)) {
    if (value === '' || value === null || value === undefined) return '';
    const numberValue = Number(value);
    return Number.isFinite(numberValue) ? numberValue : '';
  }
  return value === null || value === undefined ? '' : String(value);
}

function parseMonitoringNumber(value) {
  if (value === '' || value === null || value === undefined) return '';
  const numberValue = Number(value);
  return Number.isFinite(numberValue) ? numberValue : '';
}

export default function SettingsMonitoring(props) {
  const { t } = useTranslation();
  const [loading, setLoading] = useState(false);
  const [inputs, setInputs] = useState(defaultMonitoringInputs);
  const refForm = useRef();
  const [inputsRow, setInputsRow] = useState(inputs);
  const parsedAutoDisableStatusCodes = parseHttpStatusCodeRules(
    inputs.AutomaticDisableStatusCodes || '',
  );
  const parsedAutoRetryStatusCodes = parseHttpStatusCodeRules(
    inputs.AutomaticRetryStatusCodes || '',
  );

  function onSubmit() {
    const updateArray = compareObjects(inputsRow, inputs);
    if (!updateArray.length) return showWarning(t('你似乎并没有修改什么'));
    if (!parsedAutoDisableStatusCodes.ok) {
      const details =
        parsedAutoDisableStatusCodes.invalidTokens &&
        parsedAutoDisableStatusCodes.invalidTokens.length > 0
          ? `: ${parsedAutoDisableStatusCodes.invalidTokens.join(', ')}`
          : '';
      return showError(`${t('自动禁用状态码格式不正确')}${details}`);
    }
    if (!parsedAutoRetryStatusCodes.ok) {
      const details =
        parsedAutoRetryStatusCodes.invalidTokens &&
        parsedAutoRetryStatusCodes.invalidTokens.length > 0
          ? `: ${parsedAutoRetryStatusCodes.invalidTokens.join(', ')}`
          : '';
      return showError(`${t('自动重试状态码格式不正确')}${details}`);
    }
    const invalidNumericKey = updateArray.find(
      (item) =>
        numericMonitoringKeys.has(item.key) &&
        (inputs[item.key] === '' || !Number.isFinite(Number(inputs[item.key]))),
    );
    if (invalidNumericKey) {
      return showError(t('动态权重参数必须是有效数字'));
    }
    const minMultiplier = Number(
      inputs['monitor_setting.dynamic_channel_weight_min_multiplier'],
    );
    const maxMultiplier = Number(
      inputs['monitor_setting.dynamic_channel_weight_max_multiplier'],
    );
    if (
      Number.isFinite(minMultiplier) &&
      Number.isFinite(maxMultiplier) &&
      minMultiplier > maxMultiplier
    ) {
      return showError(t('动态权重下限不能大于上限'));
    }
    const requestQueue = updateArray.map((item) => {
      let value = '';
      if (typeof inputs[item.key] === 'boolean') {
        value = String(inputs[item.key]);
      } else {
        const normalizedMap = {
          AutomaticDisableStatusCodes: parsedAutoDisableStatusCodes.normalized,
          AutomaticRetryStatusCodes: parsedAutoRetryStatusCodes.normalized,
        };
        value =
          normalizedMap[item.key] ??
          normalizeMonitoringValue(item.key, inputs[item.key]);
      }
      return API.put('/api/option/', {
        key: item.key,
        value,
      });
    });
    setLoading(true);
    Promise.all(requestQueue)
      .then((res) => {
        const failedResponse = res.find((response) => !response?.data?.success);
        if (failedResponse) {
          return showError(
            failedResponse.data?.message ||
              (res.length > 1
                ? t('部分保存失败，请重试')
                : t('保存失败，请重试')),
          );
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
    // Keep defaults for keys that predate this setting page or have not yet
    // been persisted. Otherwise newly entered fields are invisible to the
    // change detector and the page reports that nothing was modified.
    const currentInputs = { ...defaultMonitoringInputs };
    for (let key in props.options) {
      if (Object.prototype.hasOwnProperty.call(defaultMonitoringInputs, key)) {
        currentInputs[key] = normalizeMonitoringValue(key, props.options[key]);
      }
    }
    setInputs(currentInputs);
    setInputsRow(structuredClone(currentInputs));
    refForm.current?.setValues(currentInputs);
  }, [props.options]);

  return (
    <>
      <Spin spinning={loading}>
        <Form
          values={inputs}
          getFormApi={(formAPI) => (refForm.current = formAPI)}
          style={{ marginBottom: 15 }}
        >
          <Form.Section text={t('监控设置')}>
            <Row gutter={16}>
              <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                <Form.Switch
                  field={'monitor_setting.auto_test_channel_enabled'}
                  label={t('定时测试所有通道')}
                  size='default'
                  checkedText='｜'
                  uncheckedText='〇'
                  onChange={(value) =>
                    setInputs({
                      ...inputs,
                      'monitor_setting.auto_test_channel_enabled': value,
                    })
                  }
                />
              </Col>
              <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                <Form.InputNumber
                  label={t('自动测试所有通道间隔时间')}
                  step={1}
                  min={1}
                  suffix={t('分钟')}
                  extraText={t('每隔多少分钟测试一次所有通道')}
                  placeholder={''}
                  field={'monitor_setting.auto_test_channel_minutes'}
                  onChange={(value) =>
                    setInputs({
                      ...inputs,
                      'monitor_setting.auto_test_channel_minutes':
                        parseMonitoringNumber(value),
                    })
                  }
                />
              </Col>
            </Row>
            <Row gutter={16}>
              <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                <Form.Switch
                  field={'monitor_setting.dynamic_channel_weight_enabled'}
                  label={t('启用动态渠道权重')}
                  size='default'
                  checkedText='｜'
                  uncheckedText='〇'
                  onChange={(value) =>
                    setInputs({
                      ...inputs,
                      'monitor_setting.dynamic_channel_weight_enabled': value,
                    })
                  }
                />
              </Col>
              <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                <Form.InputNumber
                  label={t('动态权重统计窗口')}
                  step={1}
                  min={1}
                  suffix={t('分钟')}
                  extraText={t('按最近真实请求的首 token 和成功结果计算')}
                  field={
                    'monitor_setting.dynamic_channel_weight_window_minutes'
                  }
                  onChange={(value) =>
                    setInputs({
                      ...inputs,
                      'monitor_setting.dynamic_channel_weight_window_minutes':
                        parseMonitoringNumber(value),
                    })
                  }
                />
              </Col>
              <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                <Form.InputNumber
                  label={t('动态权重最少样本数')}
                  step={1}
                  min={1}
                  suffix={t('条')}
                  extraText={t('样本不足时保持人工配置权重')}
                  field={'monitor_setting.dynamic_channel_weight_min_samples'}
                  onChange={(value) =>
                    setInputs({
                      ...inputs,
                      'monitor_setting.dynamic_channel_weight_min_samples':
                        parseMonitoringNumber(value),
                    })
                  }
                />
              </Col>
            </Row>
            <Row gutter={16}>
              <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                <Form.InputNumber
                  label={t('目标首 token 时长')}
                  step={500}
                  min={100}
                  suffix={t('毫秒')}
                  extraText={t('低于目标会适度增权，高于目标会降权')}
                  field={'monitor_setting.dynamic_channel_weight_target_frt_ms'}
                  onChange={(value) =>
                    setInputs({
                      ...inputs,
                      'monitor_setting.dynamic_channel_weight_target_frt_ms':
                        parseMonitoringNumber(value),
                    })
                  }
                />
              </Col>
              <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                <Form.InputNumber
                  label={t('5xx 失败惩罚')}
                  step={0.1}
                  min={0}
                  max={10}
                  extraText={t('数值越高，近期 5xx 对权重影响越大')}
                  field={'monitor_setting.dynamic_channel_weight_error_penalty'}
                  onChange={(value) =>
                    setInputs({
                      ...inputs,
                      'monitor_setting.dynamic_channel_weight_error_penalty':
                        parseMonitoringNumber(value),
                    })
                  }
                />
              </Col>
              <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                <Form.InputNumber
                  label={t('429 额外惩罚')}
                  step={0.1}
                  min={0}
                  max={10}
                  extraText={t('数值越高，限流渠道会更快降权')}
                  field={'monitor_setting.dynamic_channel_weight_429_penalty'}
                  onChange={(value) =>
                    setInputs({
                      ...inputs,
                      'monitor_setting.dynamic_channel_weight_429_penalty':
                        parseMonitoringNumber(value),
                    })
                  }
                />
              </Col>
            </Row>
            <Row gutter={16}>
              <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                <Form.InputNumber
                  label={t('动态权重下限')}
                  step={0.05}
                  min={0.05}
                  max={10}
                  suffix={'x'}
                  extraText={t('建议 0.25，避免异常渠道完全失去流量')}
                  field={
                    'monitor_setting.dynamic_channel_weight_min_multiplier'
                  }
                  onChange={(value) =>
                    setInputs({
                      ...inputs,
                      'monitor_setting.dynamic_channel_weight_min_multiplier':
                        parseMonitoringNumber(value),
                    })
                  }
                />
              </Col>
              <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                <Form.InputNumber
                  label={t('动态权重上限')}
                  step={0.05}
                  min={0.05}
                  max={10}
                  suffix={'x'}
                  extraText={t('建议 2，避免健康渠道吃满全部流量')}
                  field={
                    'monitor_setting.dynamic_channel_weight_max_multiplier'
                  }
                  onChange={(value) =>
                    setInputs({
                      ...inputs,
                      'monitor_setting.dynamic_channel_weight_max_multiplier':
                        parseMonitoringNumber(value),
                    })
                  }
                />
              </Col>
            </Row>
            <Row gutter={16}>
              <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                <Form.InputNumber
                  label={t('测试所有渠道的最长响应时间')}
                  step={1}
                  min={0}
                  suffix={t('秒')}
                  extraText={t(
                    '当运行通道全部测试时，超过此时间将自动禁用通道',
                  )}
                  placeholder={''}
                  field={'ChannelDisableThreshold'}
                  onChange={(value) =>
                    setInputs({
                      ...inputs,
                      ChannelDisableThreshold: String(value),
                    })
                  }
                />
              </Col>
              <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                <Form.InputNumber
                  label={t('额度提醒阈值')}
                  step={1}
                  min={0}
                  suffix={'Token'}
                  extraText={t('低于此额度时将发送邮件提醒用户')}
                  placeholder={''}
                  field={'QuotaRemindThreshold'}
                  onChange={(value) =>
                    setInputs({
                      ...inputs,
                      QuotaRemindThreshold: String(value),
                    })
                  }
                />
              </Col>
            </Row>
            <Row gutter={16}>
              <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                <Form.Switch
                  field={'AutomaticDisableChannelEnabled'}
                  label={t('失败时自动禁用通道')}
                  size='default'
                  checkedText='｜'
                  uncheckedText='〇'
                  onChange={(value) => {
                    setInputs({
                      ...inputs,
                      AutomaticDisableChannelEnabled: value,
                    });
                  }}
                />
              </Col>
              <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                <Form.Switch
                  field={'AutomaticEnableChannelEnabled'}
                  label={t('成功时自动启用通道')}
                  size='default'
                  checkedText='｜'
                  uncheckedText='〇'
                  onChange={(value) =>
                    setInputs({
                      ...inputs,
                      AutomaticEnableChannelEnabled: value,
                    })
                  }
                />
              </Col>
            </Row>
            <Row gutter={16}>
              <Col xs={24} sm={16}>
                <HttpStatusCodeRulesInput
                  label={t('自动禁用状态码')}
                  placeholder={t('例如：401, 403, 429, 500-599')}
                  extraText={t(
                    '支持填写单个状态码或范围（含首尾），使用逗号分隔',
                  )}
                  field={'AutomaticDisableStatusCodes'}
                  onChange={(value) =>
                    setInputs({ ...inputs, AutomaticDisableStatusCodes: value })
                  }
                  parsed={parsedAutoDisableStatusCodes}
                  invalidText={t('自动禁用状态码格式不正确')}
                />
                <HttpStatusCodeRulesInput
                  label={t('自动重试状态码')}
                  placeholder={t('例如：401, 403, 429, 500-599')}
                  extraText={t(
                    '支持填写单个状态码或范围（含首尾），使用逗号分隔；504 和 524 始终不重试，不受此处配置影响',
                  )}
                  field={'AutomaticRetryStatusCodes'}
                  onChange={(value) =>
                    setInputs({ ...inputs, AutomaticRetryStatusCodes: value })
                  }
                  parsed={parsedAutoRetryStatusCodes}
                  invalidText={t('自动重试状态码格式不正确')}
                />
                <Form.TextArea
                  label={t('自动禁用关键词')}
                  placeholder={t('一行一个，不区分大小写')}
                  extraText={t(
                    '当上游通道返回错误中包含这些关键词时（不区分大小写），自动禁用通道',
                  )}
                  field={'AutomaticDisableKeywords'}
                  autosize={{ minRows: 6, maxRows: 12 }}
                  onChange={(value) =>
                    setInputs({ ...inputs, AutomaticDisableKeywords: value })
                  }
                />
              </Col>
            </Row>
            <Row>
              <Button size='default' onClick={onSubmit}>
                {t('保存监控设置')}
              </Button>
            </Row>
          </Form.Section>
        </Form>
      </Spin>
    </>
  );
}
