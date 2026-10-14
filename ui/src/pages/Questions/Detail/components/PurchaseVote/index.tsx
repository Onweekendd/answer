/*
 * Licensed to the Apache Software Foundation (ASF) under one
 * or more contributor license agreements.  See the NOTICE file
 * distributed with this work for additional information
 * regarding copyright ownership.  The ASF licenses this file
 * to you under the Apache License, Version 2.0 (the
 * "License"); you may not use this file except in compliance
 * with the License.  You may obtain a copy of the License at
 *
 *   http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing,
 * software distributed under the License is distributed on an
 * "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
 * KIND, either express or implied.  See the License for the
 * specific language governing permissions and limitations
 * under the License.
 */

import { FC, memo, useEffect, useState } from 'react';
import {
  Button,
  Card,
  OverlayTrigger,
  ProgressBar,
  Tooltip,
} from 'react-bootstrap';
import { useTranslation } from 'react-i18next';

import type * as Type from '@/common/interface';
import { votePurchase } from '@/services';
import { guard } from '@/utils';

// Must match voteCloseAt in internal/service/purchase_vote.
const VOTE_CLOSE_AT = 10;

const OPTIONS: {
  choice: Type.PurchaseVoteChoice;
  key: 'buy' | 'wait' | 'skip';
  variant: 'success' | 'warning' | 'danger';
}[] = [
  { choice: 1, key: 'buy', variant: 'success' },
  { choice: 2, key: 'wait', variant: 'warning' },
  { choice: 3, key: 'skip', variant: 'danger' },
];

interface Tally {
  buy: number;
  wait: number;
  skip: number;
}

const tallyOf = (purchase: Type.PurchaseInfo): Tally => ({
  buy: purchase.buy_count || 0,
  wait: purchase.wait_count || 0,
  skip: purchase.skip_count || 0,
});

interface Props {
  questionId: string;
  purchase: Type.PurchaseInfo;
  /** the post's author sees the tally but cannot vote */
  isAuthor: boolean;
}

/**
 * The community's buy / wait / skip vote: a three-colour ratio bar and one
 * button per choice. It shows counts only; the post never gets a verdict, the
 * author decides.
 */
const Index: FC<Props> = ({ questionId, purchase, isAuthor }) => {
  const { t } = useTranslation('translation', {
    keyPrefix: 'purchase.vote',
  });
  const [tally, setTally] = useState<Tally>(tallyOf(purchase));
  const [myVote, setMyVote] = useState(purchase.my_vote || 0);
  const [closed, setClosed] = useState(Boolean(purchase.vote_closed));
  const [submitting, setSubmitting] = useState(false);
  const aiVote = purchase.ai_vote || 0;

  useEffect(() => {
    setTally(tallyOf(purchase));
    setMyVote(purchase.my_vote || 0);
    setClosed(Boolean(purchase.vote_closed));
  }, [purchase]);

  const total = tally.buy + tally.wait + tally.skip;
  const percent = (count: number) =>
    total ? Math.round((count * 100) / total) : 0;

  const handleVote = (choice: Type.PurchaseVoteChoice) => {
    if (!guard.tryNormalLogged(true) || choice === myVote) {
      return;
    }
    setSubmitting(true);
    votePurchase(questionId, choice)
      .then((res) => {
        setTally({
          buy: res.buy_count,
          wait: res.wait_count,
          skip: res.skip_count,
        });
        setMyVote(res.my_choice);
        setClosed(res.closed);
      })
      // the request util already shows the reason (closed, own post)
      .catch(() => {})
      .finally(() => setSubmitting(false));
  };

  let status = t('empty');
  if (closed) {
    status = t('closed', { count: total });
  } else if (total > 0) {
    status = t('total', { count: total, limit: VOTE_CLOSE_AT });
  }

  let hint = '';
  if (isAuthor) {
    hint = t('author_hint');
  } else if (myVote && !closed) {
    const mine = OPTIONS.find((o) => o.choice === myVote);
    hint = t('my_vote', { choice: mine ? t(mine.key) : '' });
  }

  return (
    <Card className="mb-4">
      <Card.Body>
        <div className="d-flex flex-wrap justify-content-between align-items-baseline gap-2 mb-2">
          <h2 className="h6 mb-0">{t('title')}</h2>
          <small className="text-secondary">{status}</small>
        </div>

        <ProgressBar className="mb-3" style={{ height: '0.75rem' }}>
          {OPTIONS.map((o) =>
            tally[o.key] > 0 ? (
              <ProgressBar
                key={o.key}
                variant={o.variant}
                now={(tally[o.key] * 100) / total}
                aria-label={`${t(o.key)} ${tally[o.key]}`}
              />
            ) : null,
          )}
        </ProgressBar>

        <div className="d-flex flex-wrap gap-2">
          {OPTIONS.map((o) => (
            <Button
              key={o.key}
              size="sm"
              variant={myVote === o.choice ? o.variant : `outline-${o.variant}`}
              active={myVote === o.choice}
              disabled={closed || isAuthor || submitting}
              onClick={() => handleVote(o.choice)}>
              {t(o.key)}
              <span className="ms-1 fw-semibold">{tally[o.key]}</span>
              {total > 0 && (
                <span className="ms-1 opacity-75">
                  {percent(tally[o.key])}%
                </span>
              )}
              {aiVote === o.choice && (
                <OverlayTrigger
                  placement="top"
                  overlay={
                    <Tooltip id={`ai-vote-${o.key}`}>{t('ai_voted')}</Tooltip>
                  }>
                  <span className="ms-1" role="img" aria-label={t('ai_voted')}>
                    🤖
                  </span>
                </OverlayTrigger>
              )}
            </Button>
          ))}
        </div>

        {hint && <div className="small text-secondary mt-2">{hint}</div>}
      </Card.Body>
    </Card>
  );
};

export default memo(Index);
