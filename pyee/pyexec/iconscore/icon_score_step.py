# Copyright 2018 ICON Foundation
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

from enum import Enum, auto

from ..base.exception import IconServiceBaseException, ExceptionCode
from ..utils import to_camel_case


class AutoValueEnum(Enum):
    # noinspection PyMethodParameters
    def _generate_next_value_(name, start, count, last_values):
        # Generates value from the camel-cased name
        return to_camel_case(name.lower())


class StepType(AutoValueEnum):
    CONTRACT_CALL = auto()
    GET = auto()
    SET = auto()
    REPLACE = auto()
    DELETE = auto()
    INPUT = auto()
    EVENT_LOG = auto()
    API_CALL = auto()


class OutOfStepException(IconServiceBaseException):
    """ An Exception which is thrown when steps are exhausted.
    """

    def __init__(self, step_limit: int, step_used: int,
                 requested_step: int, step_type: StepType) -> None:
        """Constructor

        :param step_limit: step limit of the transaction
        :param step_used: used steps in the transaction
        :param requested_step: consuming steps before the exception is thrown
        :param step_type: step type that
        the exception has been thrown when processing
        """
        self.__step_limit: int = step_limit
        self.__step_used = step_used
        self.__requested_step = requested_step
        self.__step_type = step_type

    @property
    def code(self) -> int:
        return ExceptionCode.SCORE_ERROR

    @property
    def message(self) -> str:
        """
        Returns the exception message
        :return: the exception message
        """
        return f'Out of step: {self.__step_type.value}'

    @property
    def step_limit(self) -> int:
        """
        Returns step limit of the transaction
        :return: step limit of the transaction
        """
        return self.__step_limit

    @property
    def step_used(self) -> int:
        """
        Returns used steps before the exception is thrown in the transaction
        :return: used steps in the transaction
        """
        return self.__step_used

    @property
    def requested_step(self) -> int:
        """
        Returns consuming steps before the exception is thrown.
        :return: Consuming steps before the exception is thrown.
        """
        return self.__requested_step


class IconScoreStepCounter(object):
    """ Counts steps in a transaction
    """

    def __init__(self,
                 step_costs: dict,
                 step_limit: int) -> None:
        """Constructor

        :param step_costs: a dict of base step costs
        :param step_limit: step limit for current context type
        """
        converted_step_costs = {}
        for key, value in step_costs.items():
            try:
                converted_step_costs[StepType(key)] = value
            except ValueError:
                # Pass the unknown step type
                pass
        self._step_costs: dict = converted_step_costs
        self._step_limit: int = step_limit
        self._step_used: int = 0

    @property
    def step_limit(self) -> int:
        """
        Returns step limit of the transaction
        :return: step limit of the transaction
        """
        return self._step_limit

    @property
    def step_used(self) -> int:
        """
        Returns used steps in the transaction
        :return: used steps in the transaction
        """
        return self._step_used

    def check_step_remained(self, step_type: StepType) -> int:
        """ Check if remained steps are sufficient for executing step_type,
            and returns the remained steps
        """
        required = self._step_costs.get(step_type, 0)
        remained = self._step_limit - self._step_used
        if required > remained:
            # raise OutOfStepException
            self.apply_step(step_type, 1)
        return remained

    def apply_step(self, step_type: StepType, count: int) -> int:
        """ Increases steps for given step cost
        """
        step_to_apply = self._step_costs.get(step_type, 0) * count
        if step_to_apply + self._step_used > self._step_limit:
            step_used = self._step_used
            self._step_used = self._step_limit
            raise OutOfStepException(
                self._step_limit, step_used, step_to_apply, step_type)

        self._step_used += step_to_apply
        return self._step_used

    def add_step(self, amount: int) -> int:
        # Assuming amount is always less than the current limit
        self._step_used += amount
        return self._step_used
